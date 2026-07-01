package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateAPIToken(t *testing.T) {
	plaintext, hash, prefix, err := generateAPIToken()
	if err != nil {
		t.Fatalf("generateAPIToken: %v", err)
	}

	// Prefix marker.
	if !strings.HasPrefix(plaintext, apiTokenPrefix) {
		t.Errorf("plaintext %q missing prefix %q", plaintext, apiTokenPrefix)
	}

	// Length: "ast_" + hex(32 bytes) = 4 + 64.
	wantLen := len(apiTokenPrefix) + apiTokenRandomBytes*2
	if len(plaintext) != wantLen {
		t.Errorf("plaintext length = %d, want %d", len(plaintext), wantLen)
	}

	// Body must be valid lowercase hex.
	body := strings.TrimPrefix(plaintext, apiTokenPrefix)
	if _, err := hex.DecodeString(body); err != nil {
		t.Errorf("token body is not valid hex: %v", err)
	}

	// Display prefix is the leading apiTokenDisplayLen chars.
	if prefix != plaintext[:apiTokenDisplayLen] {
		t.Errorf("prefix = %q, want %q", prefix, plaintext[:apiTokenDisplayLen])
	}

	// Hash is the hex SHA-256 of the plaintext.
	if hash != hashAPIToken(plaintext) {
		t.Errorf("returned hash does not match hashAPIToken(plaintext)")
	}
	if len(hash) != sha256.Size*2 {
		t.Errorf("hash length = %d, want %d", len(hash), sha256.Size*2)
	}
}

func TestGenerateAPIToken_Entropy(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		plaintext, hash, _, err := generateAPIToken()
		if err != nil {
			t.Fatalf("generateAPIToken: %v", err)
		}
		if _, dup := seen[plaintext]; dup {
			t.Fatalf("duplicate token generated: %q", plaintext)
		}
		seen[plaintext] = struct{}{}
		if _, dup := seen[hash]; dup {
			t.Fatalf("duplicate hash generated: %q", hash)
		}
		seen[hash] = struct{}{}
	}
}

func TestHashAPIToken(t *testing.T) {
	// Known-answer test: hashAPIToken must be plain hex SHA-256, no salt.
	sum := sha256.Sum256([]byte("ast_deadbeef"))
	want := hex.EncodeToString(sum[:])
	if got := hashAPIToken("ast_deadbeef"); got != want {
		t.Errorf("hashAPIToken = %q, want %q", got, want)
	}

	// Different inputs hash differently.
	if hashAPIToken("ast_a") == hashAPIToken("ast_b") {
		t.Errorf("distinct tokens produced identical hashes")
	}
}
