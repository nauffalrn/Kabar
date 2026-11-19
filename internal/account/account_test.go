package account

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"article-service/internal/article"
)

type fakeStore struct {
	users    map[string]User   // by email
	hashes   map[string]string // by email
	sessions map[string]Session
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]User{}, hashes: map[string]string{}, sessions: map[string]Session{}}
}

func (f *fakeStore) CreateUser(_ context.Context, u User, hash string) error {
	if _, ok := f.users[u.Email]; ok {
		return ErrEmailTaken
	}
	f.users[u.Email], f.hashes[u.Email] = u, hash
	return nil
}

func (f *fakeStore) FindUserByEmail(_ context.Context, email string) (User, string, error) {
	u, ok := f.users[email]
	if !ok {
		return User{}, "", ErrUserNotFound
	}
	return u, f.hashes[email], nil
}

func (f *fakeStore) SetRole(_ context.Context, id, role string) error {
	for e, u := range f.users {
		if u.ID == id {
			u.Role = role
			f.users[e] = u
		}
	}
	return nil
}

func (f *fakeStore) SearchUsers(context.Context, string, int) ([]UserSummary, error) { return nil, nil }

func (f *fakeStore) SetDisabled(_ context.Context, id string, disabled bool) error {
	for e, u := range f.users {
		if u.ID == id {
			if disabled {
				now := time.Now()
				u.DisabledAt = &now
			} else {
				u.DisabledAt = nil
			}
			f.users[e] = u
		}
	}
	return nil
}

func (f *fakeStore) CreateSession(_ context.Context, s Session) error {
	f.sessions[s.TokenHash] = s
	return nil
}

func (f *fakeStore) FindSessionUser(_ context.Context, hash string, now time.Time) (User, error) {
	s, ok := f.sessions[hash]
	if !ok || now.After(s.ExpiresAt) {
		return User{}, ErrSessionInvalid
	}
	for _, u := range f.users {
		if u.ID == s.UserID {
			return u, nil
		}
	}
	return User{}, ErrSessionInvalid
}

func (f *fakeStore) DeleteSession(_ context.Context, hash string) error {
	delete(f.sessions, hash)
	return nil
}

func newService() (*Service, *fakeStore) {
	store := newFakeStore()
	n := 0
	return NewService(store, func() string { n++; return "id-" + strconv.Itoa(n) }), store
}

func TestRegister_CreatesUserAndSession(t *testing.T) {
	svc, store := newService()
	u, token, err := svc.Register(context.Background(), "  Siti   Rahma ", " Siti@Contoh.ID ", "rahasia123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Siti Rahma" || u.Email != "siti@contoh.id" || u.Role != RoleUser || u.AuthorID == "" {
		t.Fatalf("unexpected user %+v", u)
	}
	if store.hashes[u.Email] == "rahasia123" {
		t.Fatal("password must never be stored in plain text")
	}
	if _, ok := store.sessions[token]; ok {
		t.Fatal("the raw token must not be stored, only its hash")
	}
	got, err := svc.Authenticate(context.Background(), token)
	if err != nil || got.ID != u.ID {
		t.Fatalf("token should authenticate the new user: %v", err)
	}
}

func TestRegister_ValidatesEveryField(t *testing.T) {
	svc, _ := newService()
	_, _, err := svc.Register(context.Background(), "", "bukan-email", "pendek")
	var verr *article.ValidationError
	if !errors.As(err, &verr) || len(verr.Fields) != 3 {
		t.Fatalf("want 3 field errors, got %v", err)
	}
}

func TestLogin(t *testing.T) {
	svc, store := newService()
	ctx := context.Background()
	u, _, err := svc.Register(ctx, "Budi", "budi@contoh.id", "rahasia123")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("correct password", func(t *testing.T) {
		if _, token, err := svc.Login(ctx, "BUDI@contoh.id", "rahasia123"); err != nil || token == "" {
			t.Fatalf("login failed: %v", err)
		}
	})
	t.Run("wrong password and unknown email look the same", func(t *testing.T) {
		_, _, errWrong := svc.Login(ctx, "budi@contoh.id", "salah12345")
		_, _, errUnknown := svc.Login(ctx, "siapa@contoh.id", "rahasia123")
		if !errors.Is(errWrong, ErrInvalidCredentials) || !errors.Is(errUnknown, ErrInvalidCredentials) {
			t.Fatalf("got %v and %v", errWrong, errUnknown)
		}
	})
	t.Run("blocked user", func(t *testing.T) {
		_ = store.SetDisabled(ctx, u.ID, true)
		if _, _, err := svc.Login(ctx, "budi@contoh.id", "rahasia123"); !errors.Is(err, ErrUserDisabled) {
			t.Fatalf("want ErrUserDisabled, got %v", err)
		}
	})
}

func TestAuthenticate_ExpiredSession(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	_, token, err := svc.Register(ctx, "Budi", "budi@contoh.id", "rahasia123")
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Now().Add(SessionTTL + time.Hour) }
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("want ErrSessionInvalid, got %v", err)
	}
}

func TestEnsureAdmin_IsIdempotent(t *testing.T) {
	svc, store := newService()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := svc.EnsureAdmin(ctx, "Admin", "admin@contoh.id", "rahasia123"); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.users) != 1 || !store.users["admin@contoh.id"].IsAdmin() {
		t.Fatalf("want exactly one admin, got %+v", store.users)
	}
	if bcrypt.CompareHashAndPassword([]byte(store.hashes["admin@contoh.id"]), []byte("rahasia123")) != nil {
		t.Fatal("admin password should be stored as a bcrypt hash")
	}
}

func TestModeration_RequiresAdmin(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	user := &article.Actor{UserID: "u1"}
	if err := svc.SetBlocked(ctx, user, "u2", true); !errors.Is(err, article.ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
	if err := svc.SetBlocked(ctx, nil, "u2", true); !errors.Is(err, article.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
	admin := &article.Actor{UserID: "a1", Admin: true}
	var verr *article.ValidationError
	if err := svc.SetBlocked(ctx, admin, "a1", true); !errors.As(err, &verr) {
		t.Fatalf("admins should not block themselves, got %v", err)
	}
}
