package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// helper: create a test org; unique name avoids collisions between parallel tests.
func createTestOrg(t *testing.T, s *OrgStore, suffix string) string {
	t.Helper()
	org, err := s.CreateOrg(context.Background(), "customer-test-org-"+suffix)
	if err != nil {
		t.Fatalf("createTestOrg: %v", err)
	}
	return org.ID
}

func TestCustomerStore_Create(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	c, err := cs.CreateCustomer(ctx, orgID, "ext-001", ptrStr("Alice"), json.RawMessage(`{"tier":"gold"}`))
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if c.ID == "" {
		t.Error("ID should not be empty")
	}
	if c.ExternalID != "ext-001" {
		t.Errorf("ExternalID: want ext-001, got %s", c.ExternalID)
	}
	if c.Name == nil || *c.Name != "Alice" {
		t.Errorf("Name: want Alice, got %v", c.Name)
	}
	if c.OrgID != orgID {
		t.Errorf("OrgID mismatch")
	}
}

func TestCustomerStore_CreateDefaultMetadata(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	c, err := cs.CreateCustomer(ctx, orgID, "ext-default-meta", nil, nil)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if string(c.Metadata) != "{}" {
		t.Errorf("metadata default: want {}, got %s", c.Metadata)
	}
}

func TestCustomerStore_Get(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	created, err := cs.CreateCustomer(ctx, orgID, "ext-get", ptrStr("Bob"), nil)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	got, err := cs.GetCustomer(ctx, orgID, created.ID)
	if err != nil {
		t.Fatalf("GetCustomer: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch")
	}
	if got.ExternalID != "ext-get" {
		t.Errorf("ExternalID mismatch")
	}
}

// DoD: get a customer from a different org returns not-found (tenancy isolation).
func TestCustomerStore_GetCrossOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	c, err := cs.CreateCustomer(ctx, orgA, "ext-cross", nil, nil)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	// Looking up orgA's customer with orgB's context must return not-found.
	_, err = cs.GetCustomer(ctx, orgB, c.ID)
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Errorf("cross-org get: want ErrCustomerNotFound, got %v", err)
	}
}

func TestCustomerStore_List(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	for i := range 3 {
		extID := "list-ext-" + string(rune('a'+i))
		if _, err := cs.CreateCustomer(ctx, orgID, extID, nil, nil); err != nil {
			t.Fatalf("CreateCustomer %d: %v", i, err)
		}
	}

	customers, next, err := cs.ListCustomers(ctx, orgID, 100, "")
	if err != nil {
		t.Fatalf("ListCustomers: %v", err)
	}
	if len(customers) < 3 {
		t.Errorf("want ≥3 customers, got %d", len(customers))
	}
	if next != "" {
		t.Errorf("want empty nextCursor for small result, got %q", next)
	}
	// All returned customers must belong to this org.
	for _, c := range customers {
		if c.OrgID != orgID {
			t.Errorf("ListCustomers returned customer from wrong org: %s", c.OrgID)
		}
	}
}

func TestCustomerStore_ListPagination(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	for i := range 5 {
		extID := "pag-ext-" + string(rune('a'+i))
		if _, err := cs.CreateCustomer(ctx, orgID, extID, nil, nil); err != nil {
			t.Fatalf("CreateCustomer %d: %v", i, err)
		}
	}

	page1, cursor1, err := cs.ListCustomers(ctx, orgID, 3, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("page 1: want 3, got %d", len(page1))
	}
	if cursor1 == "" {
		t.Error("page 1: want non-empty cursor")
	}

	page2, cursor2, err := cs.ListCustomers(ctx, orgID, 3, cursor1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) < 2 {
		t.Errorf("page 2: want ≥2, got %d", len(page2))
	}
	_ = cursor2 // may or may not be empty depending on whether there are exactly 5 rows

	// IDs on page 2 must not overlap page 1.
	seen := map[string]bool{}
	for _, c := range page1 {
		seen[c.ID] = true
	}
	for _, c := range page2 {
		if seen[c.ID] {
			t.Errorf("duplicate customer %s across pages", c.ID)
		}
	}
}

// DoD: duplicate externalId in the same org returns ErrDuplicateExternalID.
func TestCustomerStore_DuplicateExternalID_SameOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	if _, err := cs.CreateCustomer(ctx, orgID, "dup-ext", nil, nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := cs.CreateCustomer(ctx, orgID, "dup-ext", nil, nil)
	if !errors.Is(err, ErrDuplicateExternalID) {
		t.Errorf("duplicate in same org: want ErrDuplicateExternalID, got %v", err)
	}
}

// DoD: same externalId in a different org is allowed.
func TestCustomerStore_DuplicateExternalID_DifferentOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	if _, err := cs.CreateCustomer(ctx, orgA, "shared-ext", nil, nil); err != nil {
		t.Fatalf("create in org A: %v", err)
	}
	if _, err := cs.CreateCustomer(ctx, orgB, "shared-ext", nil, nil); err != nil {
		t.Errorf("create same externalId in org B: want nil error, got %v", err)
	}
}

func TestCustomerStore_Update(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())
	c, err := cs.CreateCustomer(ctx, orgID, "upd-ext", ptrStr("Old Name"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	newName := "New Name"
	updated, err := cs.UpdateCustomer(ctx, orgID, c.ID, CustomerUpdate{
		SetName: true, Name: &newName,
		SetMetadata: true, Metadata: json.RawMessage(`{"plan":"pro"}`),
	})
	if err != nil {
		t.Fatalf("UpdateCustomer: %v", err)
	}
	if updated.Name == nil || *updated.Name != "New Name" {
		t.Errorf("name: want New Name, got %v", updated.Name)
	}
	// Postgres normalizes JSONB whitespace; compare parsed values.
	var meta map[string]string
	if err := json.Unmarshal(updated.Metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["plan"] != "pro" {
		t.Errorf("metadata[plan]: want pro, got %s", meta["plan"])
	}
}

func TestCustomerStore_UpdateNotFound(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgID := createTestOrg(t, os, t.Name())

	_, err := cs.UpdateCustomer(ctx, orgID, "00000000-0000-0000-0000-000000000000", CustomerUpdate{
		SetName: true, Name: ptrStr("X"),
	})
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Errorf("update non-existent: want ErrCustomerNotFound, got %v", err)
	}
}

func TestCustomerStore_UpdateCrossOrg(t *testing.T) {
	pool := getTestPool(t)
	os := NewOrgStore(pool)
	cs := NewCustomerStore(pool)
	ctx := context.Background()

	orgA := createTestOrg(t, os, t.Name()+"A")
	orgB := createTestOrg(t, os, t.Name()+"B")

	c, err := cs.CreateCustomer(ctx, orgA, "cross-upd-ext", nil, nil)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	// Updating orgA's customer with orgB's context must fail.
	_, err = cs.UpdateCustomer(ctx, orgB, c.ID, CustomerUpdate{
		SetName: true, Name: ptrStr("Hijacked"),
	})
	if !errors.Is(err, ErrCustomerNotFound) {
		t.Errorf("cross-org update: want ErrCustomerNotFound, got %v", err)
	}
}

func ptrStr(s string) *string { return &s }
