package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrKeyNotFound is returned by ResolveKey when the key does not exist or is revoked.
var ErrKeyNotFound = errors.New("api key not found or revoked")

// Org is a tenant organization.
type Org struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// OrgStore handles organization and API key persistence.
// It implements auth.KeyResolver via ResolveKey.
type OrgStore struct {
	pool *pgxpool.Pool
}

func NewOrgStore(pool *pgxpool.Pool) *OrgStore {
	return &OrgStore{pool: pool}
}

// CreateOrg inserts a new organization and returns it.
func (s *OrgStore) CreateOrg(ctx context.Context, name string) (*Org, error) {
	var o Org
	err := s.pool.QueryRow(ctx,
		`INSERT INTO organizations (name) VALUES ($1) RETURNING id, name, created_at`,
		name,
	).Scan(&o.ID, &o.Name, &o.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create org: %w", err)
	}
	return &o, nil
}

// CreateAPIKey inserts a new API key record (hash + prefix only; raw key is never stored).
// Returns the new key's UUID.
func (s *OrgStore) CreateAPIKey(ctx context.Context, orgID, name, keyHash, keyPrefix string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id, name, key_hash, key_prefix)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		orgID, name, keyHash, keyPrefix,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create api key: %w", err)
	}
	return id, nil
}

// ResolveKey looks up the org ID for a given key hash.
// Returns ErrKeyNotFound if the key does not exist or has been revoked.
// The first query parameter is always the key_hash — org_id tenancy is enforced
// by the result: callers must use the returned org_id, never a caller-supplied one.
func (s *OrgStore) ResolveKey(ctx context.Context, keyHash string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx,
		`SELECT org_id FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`,
		keyHash,
	).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve key: %w", err)
	}
	return orgID, nil
}

// RevokeAPIKey sets revoked_at on a key, scoped to the owning org for safety.
func (s *OrgStore) RevokeAPIKey(ctx context.Context, orgID, keyID string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND org_id = $2`,
		keyID, orgID,
	)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found: %w", ErrKeyNotFound)
	}
	return nil
}
