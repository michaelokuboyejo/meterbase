package dashauth_test

import (
	"testing"

	"github.com/mykelokuboyejo/meterbase/apps/api/internal/dashauth"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := dashauth.HashPassword("s3cr3t!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if dashauth.CheckPassword(hash, "s3cr3t!") != nil {
		t.Fatal("expected correct password to match")
	}
	if dashauth.CheckPassword(hash, "wrong") == nil {
		t.Fatal("expected wrong password to fail")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	raw1, hash1, err := dashauth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	raw2, hash2, err := dashauth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate second: %v", err)
	}
	if raw1 == raw2 {
		t.Fatal("raw tokens must be unique")
	}
	if hash1 == hash2 {
		t.Fatal("hashes must be unique")
	}
	if raw1 == hash1 {
		t.Fatal("raw token and its hash must differ")
	}
}
