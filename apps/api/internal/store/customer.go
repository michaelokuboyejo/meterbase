package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrCustomerNotFound = errors.New("customer not found")
var ErrDuplicateExternalID = errors.New("externalId already exists for this org")

// Customer is a billable entity belonging to an org.
type Customer struct {
	ID         string
	OrgID      string
	ExternalID string
	Name       *string
	Metadata   json.RawMessage
	CreatedAt  time.Time
}

// CustomerUpdate describes which fields to change on a PATCH.
// Only fields with the corresponding Set* = true are written.
type CustomerUpdate struct {
	SetName     bool
	Name        *string
	SetMetadata bool
	Metadata    json.RawMessage
}

// CustomerStore handles customer persistence.
type CustomerStore struct {
	pool *pgxpool.Pool
}

func NewCustomerStore(pool *pgxpool.Pool) *CustomerStore {
	return &CustomerStore{pool: pool}
}

// CreateCustomer inserts a new customer scoped to orgID.
// Returns ErrDuplicateExternalID on (org_id, external_id) conflict.
func (s *CustomerStore) CreateCustomer(ctx context.Context, orgID, externalID string, name *string, metadata json.RawMessage) (*Customer, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	var c Customer
	var metaBytes []byte
	err := s.pool.QueryRow(ctx,
		`INSERT INTO customers (org_id, external_id, name, metadata)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, external_id, name, metadata, created_at`,
		orgID, externalID, name, []byte(metadata),
	).Scan(&c.ID, &c.OrgID, &c.ExternalID, &c.Name, &metaBytes, &c.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateExternalID
		}
		return nil, fmt.Errorf("create customer: %w", err)
	}
	c.Metadata = json.RawMessage(metaBytes)
	return &c, nil
}

// GetCustomer fetches a single customer by ID, scoped to orgID.
// Returns ErrCustomerNotFound if the customer does not exist or belongs to a different org.
func (s *CustomerStore) GetCustomer(ctx context.Context, orgID, customerID string) (*Customer, error) {
	var c Customer
	var metaBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, org_id, external_id, name, metadata, created_at
		 FROM customers WHERE org_id = $1 AND id = $2`,
		orgID, customerID,
	).Scan(&c.ID, &c.OrgID, &c.ExternalID, &c.Name, &metaBytes, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get customer: %w", err)
	}
	c.Metadata = json.RawMessage(metaBytes)
	return &c, nil
}

// ListCustomers returns a page of customers for orgID, ordered by (created_at ASC, id ASC).
// cursor is an opaque token from the previous response's nextCursor; empty means first page.
// Returns the items and the next page cursor (empty when there is no next page).
func (s *CustomerStore) ListCustomers(ctx context.Context, orgID string, limit int, cursor string) ([]*Customer, string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var (
		rows pgx.Rows
		err  error
	)
	if cursor == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, external_id, name, metadata, created_at
			 FROM customers WHERE org_id = $1
			 ORDER BY created_at ASC, id ASC
			 LIMIT $2`,
			orgID, limit+1,
		)
	} else {
		ct, cid, decErr := decodeCursor(cursor)
		if decErr != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", decErr)
		}
		rows, err = s.pool.Query(ctx,
			`SELECT id, org_id, external_id, name, metadata, created_at
			 FROM customers
			 WHERE org_id = $1 AND (
			   created_at > $2
			   OR (created_at = $2 AND id > $3::uuid)
			 )
			 ORDER BY created_at ASC, id ASC
			 LIMIT $4`,
			orgID, ct, cid, limit+1,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	var customers []*Customer
	for rows.Next() {
		var c Customer
		var metaBytes []byte
		if err := rows.Scan(&c.ID, &c.OrgID, &c.ExternalID, &c.Name, &metaBytes, &c.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan customer: %w", err)
		}
		c.Metadata = json.RawMessage(metaBytes)
		customers = append(customers, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list customers: %w", err)
	}

	var nextCursor string
	if len(customers) > limit {
		customers = customers[:limit]
		last := customers[len(customers)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return customers, nextCursor, nil
}

// UpdateCustomer applies the non-zero fields of upd to the customer, scoped to orgID.
// Returns ErrCustomerNotFound if the customer does not exist or belongs to a different org.
func (s *CustomerStore) UpdateCustomer(ctx context.Context, orgID, customerID string, upd CustomerUpdate) (*Customer, error) {
	if upd.SetMetadata && len(upd.Metadata) == 0 {
		upd.Metadata = json.RawMessage("{}")
	}
	var metadataArg []byte
	if upd.SetMetadata {
		metadataArg = []byte(upd.Metadata)
	}

	var c Customer
	var metaBytes []byte
	err := s.pool.QueryRow(ctx,
		`UPDATE customers
		 SET name     = CASE WHEN $3 THEN $4      ELSE name     END,
		     metadata = CASE WHEN $5 THEN $6::jsonb ELSE metadata END
		 WHERE org_id = $1 AND id = $2
		 RETURNING id, org_id, external_id, name, metadata, created_at`,
		orgID, customerID,
		upd.SetName, upd.Name,
		upd.SetMetadata, metadataArg,
	).Scan(&c.ID, &c.OrgID, &c.ExternalID, &c.Name, &metaBytes, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update customer: %w", err)
	}
	c.Metadata = json.RawMessage(metaBytes)
	return &c, nil
}

// cursorPayload is the JSON-encoded payload inside the opaque cursor.
type cursorPayload struct {
	T int64  `json:"t"` // unix nanoseconds
	I string `json:"i"` // customer UUID
}

func encodeCursor(t time.Time, id string) string {
	b, _ := json.Marshal(cursorPayload{T: t.UnixNano(), I: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("base64 decode: %w", err)
	}
	var cp cursorPayload
	if err := json.Unmarshal(b, &cp); err != nil {
		return time.Time{}, "", fmt.Errorf("cursor json: %w", err)
	}
	return time.Unix(0, cp.T).UTC(), cp.I, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
