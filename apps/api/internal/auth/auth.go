package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type contextKey int

const orgIDKey contextKey = 0

// GenerateKey creates a new API key triple: raw key (show once), display prefix, and hash (store).
// rawKey starts with "mb_" and is 67 chars total (3 + 64 hex chars from 32 random bytes).
// prefix is the first 12 chars — safe to display, reveals nothing about the secret portion.
// keyHash is the SHA-256 hex digest; only this is stored in the database.
func GenerateKey() (rawKey, prefix, keyHash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate key bytes: %w", err)
	}
	rawKey = "mb_" + hex.EncodeToString(b)
	prefix = rawKey[:12]
	keyHash = HashKey(rawKey)
	return
}

// HashKey returns the SHA-256 hex digest of a raw API key.
// Used both when storing a new key and when verifying an incoming request.
func HashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// OrgIDFromContext retrieves the authenticated org ID placed by the auth middleware.
func OrgIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(orgIDKey).(string)
	return v, ok && v != ""
}

func withOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgIDKey, orgID)
}
