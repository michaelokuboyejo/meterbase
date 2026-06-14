package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mykelokuboyejo/meterbase/apps/api/internal/auth"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatalf("getTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestOrgStore_CreateOrg(t *testing.T) {
	pool := getTestPool(t)
	s := NewOrgStore(pool)
	ctx := context.Background()

	org, err := s.CreateOrg(ctx, "test-org-"+t.Name())
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if org.ID == "" {
		t.Error("org.ID should not be empty")
	}
	if org.Name == "" {
		t.Error("org.Name should not be empty")
	}
	if org.CreatedAt.IsZero() {
		t.Error("org.CreatedAt should not be zero")
	}
}

func TestOrgStore_CreateAndResolveKey(t *testing.T) {
	pool := getTestPool(t)
	s := NewOrgStore(pool)
	ctx := context.Background()

	org, err := s.CreateOrg(ctx, "org-for-key-test")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}

	rawKey, prefix, keyHash, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	_ = rawKey

	keyID, err := s.CreateAPIKey(ctx, org.ID, "my-key", keyHash, prefix)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if keyID == "" {
		t.Error("keyID should not be empty")
	}

	resolvedOrgID, err := s.ResolveKey(ctx, keyHash)
	if err != nil {
		t.Fatalf("ResolveKey: %v", err)
	}
	if resolvedOrgID != org.ID {
		t.Errorf("ResolveKey: expected org %q, got %q", org.ID, resolvedOrgID)
	}
}

func TestOrgStore_RevokedKey(t *testing.T) {
	pool := getTestPool(t)
	s := NewOrgStore(pool)
	ctx := context.Background()

	org, err := s.CreateOrg(ctx, "org-for-revoke-test")
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}

	_, prefix, keyHash, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	keyID, err := s.CreateAPIKey(ctx, org.ID, "revoke-me", keyHash, prefix)
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	// Key should resolve before revocation
	if _, err := s.ResolveKey(ctx, keyHash); err != nil {
		t.Fatalf("ResolveKey before revoke: %v", err)
	}

	if err := s.RevokeAPIKey(ctx, org.ID, keyID); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}

	// Key should NOT resolve after revocation
	if _, err := s.ResolveKey(ctx, keyHash); err == nil {
		t.Error("ResolveKey: expected error for revoked key, got nil")
	}
}

// TestOrgStore_CrossOrgIsolation is DoD test 3: a key for org A must never resolve to org B.
func TestOrgStore_CrossOrgIsolation(t *testing.T) {
	pool := getTestPool(t)
	s := NewOrgStore(pool)
	ctx := context.Background()

	orgA, err := s.CreateOrg(ctx, "isolation-org-a")
	if err != nil {
		t.Fatalf("CreateOrg A: %v", err)
	}
	orgB, err := s.CreateOrg(ctx, "isolation-org-b")
	if err != nil {
		t.Fatalf("CreateOrg B: %v", err)
	}

	if orgA.ID == orgB.ID {
		t.Fatal("org IDs should be distinct")
	}

	_, prefixA, hashA, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey A: %v", err)
	}
	_, prefixB, hashB, err := auth.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey B: %v", err)
	}

	if _, err := s.CreateAPIKey(ctx, orgA.ID, "key-a", hashA, prefixA); err != nil {
		t.Fatalf("CreateAPIKey A: %v", err)
	}
	if _, err := s.CreateAPIKey(ctx, orgB.ID, "key-b", hashB, prefixB); err != nil {
		t.Fatalf("CreateAPIKey B: %v", err)
	}

	resolvedA, err := s.ResolveKey(ctx, hashA)
	if err != nil {
		t.Fatalf("ResolveKey for A: %v", err)
	}
	resolvedB, err := s.ResolveKey(ctx, hashB)
	if err != nil {
		t.Fatalf("ResolveKey for B: %v", err)
	}

	if resolvedA != orgA.ID {
		t.Errorf("key A resolved to %q, want %q", resolvedA, orgA.ID)
	}
	if resolvedB != orgB.ID {
		t.Errorf("key B resolved to %q, want %q", resolvedB, orgB.ID)
	}
	// Cross-org check: key A must not give org B's ID, and vice versa
	if resolvedA == orgB.ID {
		t.Error("cross-org violation: key A resolved to org B")
	}
	if resolvedB == orgA.ID {
		t.Error("cross-org violation: key B resolved to org A")
	}
}
