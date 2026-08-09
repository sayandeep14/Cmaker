// Package token generates and hashes cmaker packs' own opaque API
// tokens - never the GitHub token itself, which is only ever used
// ephemerally to verify identity during login (see PACKS_PLAN.md §1/§6).
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// prefix marks a cmaker packs token the same way GitHub/Stripe prefix
// their own tokens - grep-able, and a quick visual sanity check that a
// string pasted somewhere really is one of these.
const prefix = "cmpk_live_"

// randomBytes is how much entropy backs each generated token - 32 bytes
// (256 bits) hex-encoded, comfortably beyond brute-force range.
const randomBytes = 32

// Generate returns a new opaque token in "cmpk_live_<64 hex chars>" form.
func Generate() (string, error) {
	buf := make([]byte, randomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

// Hash returns the sha256 hex digest of token - what's actually stored
// (api_tokens.token_hash) and compared against, so a database leak alone
// never yields a usable credential.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
