package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"article-service/internal/account"
)

type AccountRepo struct{ pool *pgxpool.Pool }

func NewAccountRepo(pool *pgxpool.Pool) *AccountRepo { return &AccountRepo{pool: pool} }

const userColumns = "u.id, u.email, au.name, u.author_id, u.role, u.created_at, u.disabled_at"

const userJoins = " FROM users u JOIN authors au ON au.id = u.author_id"

func scanUser(row pgx.Row, extra ...any) (account.User, error) {
	var u account.User
	dest := append([]any{&u.ID, &u.Email, &u.Name, &u.AuthorID, &u.Role, &u.CreatedAt, &u.DisabledAt}, extra...)
	err := row.Scan(dest...)
	return u, err
}

func (r *AccountRepo) CreateUser(ctx context.Context, u account.User, passwordHash string) error {
	err := withTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO authors (id, name, created_at) VALUES ($1, $2, $3)",
			u.AuthorID, u.Name, u.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO users (id, email, password_hash, role, author_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, u.ID, u.Email, passwordHash, u.Role, u.AuthorID, u.CreatedAt)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_lower_idx" {
		return account.ErrEmailTaken
	}
	return err
}

func (r *AccountRepo) FindUserByEmail(ctx context.Context, email string) (account.User, string, error) {
	var hash string
	u, err := scanUser(r.pool.QueryRow(ctx,
		"SELECT "+userColumns+", u.password_hash"+userJoins+" WHERE lower(u.email) = lower($1)", email), &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", account.ErrUserNotFound
	}
	return u, hash, err
}

func (r *AccountRepo) SetRole(ctx context.Context, userID, role string) error {
	_, err := r.pool.Exec(ctx, "UPDATE users SET role = $2 WHERE id = $1", userID, role)
	return err
}

// SearchUsers backs the admin table. A substring match cannot use an index,
// which is acceptable for an admin-only screen limited to 50 rows.
func (r *AccountRepo) SearchUsers(ctx context.Context, query string, limit int) ([]account.UserSummary, error) {
	pattern := "%" + escapeLike(strings.ToLower(query)) + "%"
	rows, err := r.pool.Query(ctx, `
		SELECT `+userColumns+`,
		       (SELECT count(*) FROM articles a WHERE a.author_id = u.author_id AND a.deleted_at IS NULL)`+userJoins+`
		WHERE lower(au.name) LIKE $1 OR lower(u.email) LIKE $1
		ORDER BY u.created_at DESC LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []account.UserSummary
	for rows.Next() {
		var s account.UserSummary
		if s.User, err = scanUser(rows, &s.ArticleCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *AccountRepo) SetDisabled(ctx context.Context, userID string, disabled bool) error {
	return withTx(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE users SET disabled_at = CASE WHEN $2 THEN now() END WHERE id = $1", userID, disabled)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return account.ErrUserNotFound
		}
		if !disabled {
			return nil
		}
		_, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID)
		return err
	})
}

func (r *AccountRepo) CreateSession(ctx context.Context, s account.Session) error {
	_, err := r.pool.Exec(ctx, "INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)",
		s.ID, s.UserID, s.TokenHash, s.ExpiresAt)
	return err
}

func (r *AccountRepo) FindSessionUser(ctx context.Context, tokenHash string, now time.Time) (account.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, "SELECT "+userColumns+userJoins+`
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token_hash = $1 AND s.expires_at > $2 AND u.disabled_at IS NULL`, tokenHash, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return u, account.ErrSessionInvalid
	}
	return u, err
}

func (r *AccountRepo) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
	return err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
