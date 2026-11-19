// Package account owns registration, login, sessions and user moderation.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"article-service/internal/article"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt ignores anything longer
	SessionTTL     = 30 * 24 * time.Hour
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserDisabled       = errors.New("user is blocked")
	ErrSessionInvalid     = errors.New("session invalid or expired")
	ErrUserNotFound       = errors.New("user not found")
)

type User struct {
	ID         string
	Email      string
	Name       string
	AuthorID   string
	Role       string
	CreatedAt  time.Time
	DisabledAt *time.Time
}

func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

// Actor is the user as the article domain sees it.
func (u User) Actor() *article.Actor {
	return &article.Actor{UserID: u.ID, AuthorID: u.AuthorID, Admin: u.IsAdmin()}
}

// UserSummary is one row of the moderation table.
type UserSummary struct {
	User
	ArticleCount int64
}

type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
}

type Store interface {
	// CreateUser also creates the user's author profile, in one transaction.
	CreateUser(ctx context.Context, u User, passwordHash string) error
	FindUserByEmail(ctx context.Context, email string) (User, string, error)
	SetRole(ctx context.Context, userID, role string) error
	SearchUsers(ctx context.Context, query string, limit int) ([]UserSummary, error)
	// SetDisabled also ends every session of a blocked user.
	SetDisabled(ctx context.Context, userID string, disabled bool) error
	CreateSession(ctx context.Context, s Session) error
	FindSessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
}

type Service struct {
	store Store
	newID func() string
	now   func() time.Time
}

func NewService(store Store, newID func() string) *Service {
	return &Service{store: store, newID: newID, now: time.Now}
}

func (s *Service) Register(ctx context.Context, name, email, password string) (User, string, error) {
	name = article.NormalizeAuthorName(name)
	email = normalizeEmail(email)
	if err := validateRegistration(name, email, password); err != nil {
		return User{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", fmt.Errorf("hash password: %w", err)
	}
	u := User{ID: s.newID(), Email: email, Name: name, AuthorID: s.newID(), Role: RoleUser, CreatedAt: s.now()}
	if err := s.store.CreateUser(ctx, u, string(hash)); err != nil {
		return User{}, "", err
	}
	token, err := s.startSession(ctx, u.ID)
	return u, token, err
}

func (s *Service) Login(ctx context.Context, email, password string) (User, string, error) {
	u, hash, err := s.store.FindUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, ErrUserNotFound) {
		// Same answer as a wrong password, so emails cannot be enumerated.
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, "", ErrInvalidCredentials
	}
	if u.DisabledAt != nil {
		return User{}, "", ErrUserDisabled
	}
	token, err := s.startSession(ctx, u.ID)
	return u, token, err
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrSessionInvalid
	}
	return s.store.FindSessionUser(ctx, hashToken(token), s.now())
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashToken(token))
}

func (s *Service) SearchUsers(ctx context.Context, actor *article.Actor, query string) ([]UserSummary, error) {
	if err := requireAdmin(actor); err != nil {
		return nil, err
	}
	return s.store.SearchUsers(ctx, strings.TrimSpace(query), 50)
}

func (s *Service) SetBlocked(ctx context.Context, actor *article.Actor, userID string, blocked bool) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if blocked && userID == actor.UserID {
		return article.NewValidationError("user", "admins cannot block themselves")
	}
	return s.store.SetDisabled(ctx, userID, blocked)
}

// EnsureAdmin creates or promotes the configured admin on startup. It is idempotent.
func (s *Service) EnsureAdmin(ctx context.Context, name, email, password string) error {
	email = normalizeEmail(email)
	u, _, err := s.store.FindUserByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		if u, _, err = s.Register(ctx, name, email, password); err != nil {
			return fmt.Errorf("create admin: %w", err)
		}
	} else if err != nil {
		return err
	}
	if u.IsAdmin() {
		return nil
	}
	return s.store.SetRole(ctx, u.ID, RoleAdmin)
}

// startSession stores only a hash of the token: a database leak yields no usable session.
func (s *Service) startSession(ctx context.Context, userID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	err := s.store.CreateSession(ctx, Session{
		ID: s.newID(), UserID: userID, TokenHash: hashToken(token), ExpiresAt: s.now().Add(SessionTTL),
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validateRegistration(name, email, password string) error {
	var fields []article.FieldError
	if err := article.ValidateAuthorName(name); err != nil {
		var verr *article.ValidationError
		if errors.As(err, &verr) {
			fields = append(fields, verr.Fields...)
		}
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		fields = append(fields, article.FieldError{Field: "email", Message: "must be a valid email address"})
	}
	if n := len(password); n < minPasswordLen || n > maxPasswordLen {
		fields = append(fields, article.FieldError{Field: "password", Message: "must be 8 to 72 characters"})
	}
	if len(fields) > 0 {
		return &article.ValidationError{Fields: fields}
	}
	return nil
}

func requireAdmin(a *article.Actor) error {
	if a == nil {
		return article.ErrUnauthenticated
	}
	if !a.Admin {
		return article.ErrForbidden
	}
	return nil
}
