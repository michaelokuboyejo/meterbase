package dashauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrSessionNotFound = errors.New("session not found or expired")
)

// Store handles persistence for dashboard users and sessions.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateUser inserts a new dashboard user and returns it.
func (s *Store) CreateUser(ctx context.Context, orgID, email, passwordHash, role string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (org_id, email, password_hash, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, email, role, created_at`,
		orgID, email, passwordHash, role,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &u, nil
}

// GetUserByEmailAnyOrg returns the first user matching email across all orgs.
// Used on login when the caller only knows their email (not org).
func (s *Store) GetUserByEmailAnyOrg(ctx context.Context, email string) (*User, string, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, email, role, created_at, password_hash
		 FROM users WHERE email = $1 LIMIT 1`,
		email,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrUserNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("get user: %w", err)
	}
	return &u, hash, nil
}

// CreateSession inserts a new session record. ttl is the session lifetime.
func (s *Store) CreateSession(ctx context.Context, userID, orgID, tokenHash string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (user_id, org_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		userID, orgID, tokenHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// ResolveSession looks up a non-expired session by token hash and returns the user.
// Returns ErrSessionNotFound if missing or expired.
func (s *Store) ResolveSession(ctx context.Context, tokenHash string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.org_id, u.email, u.role, u.created_at
		 FROM sessions ses
		 JOIN users u ON u.id = ses.user_id
		 WHERE ses.token_hash = $1 AND ses.expires_at > now()`,
		tokenHash,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resolve session: %w", err)
	}
	return &u, nil
}

// DeleteSession removes a session by token hash (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}
