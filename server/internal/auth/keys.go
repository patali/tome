// Package auth provides API-key generation/hashing and HTTP middleware for
// Tome's account system. Keys are random bearer tokens; only their SHA-256
// hash is stored, and lookup is hash-then-indexed-query (timing-safe by
// construction — no pairwise string comparison over secrets).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

const base62 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randBase62(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(base62)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("crypto/rand unavailable: " + err.Error()) // unrecoverable
		}
		out[i] = base62[v.Int64()]
	}
	return string(out)
}

// NewAPIKey returns a fresh user API key ("tome_" + 32 base62 chars, ~190
// bits), its storage hash, and the short prefix kept for listings/logs.
func NewAPIKey() (key, hashHex, prefix string) {
	key = "tome_" + randBase62(32)
	return key, HashKey(key), key[:10]
}

// NewInviteCode returns a single-use invite code ("tomeinv_" + 22 base62
// chars, ~128 bits). Stored plaintext so the admin can re-share it; its
// security comes from entropy + TTL + rate limiting, not secrecy at rest.
func NewInviteCode() string {
	return "tomeinv_" + randBase62(22)
}

// HashKey returns the hex SHA-256 of an API key, the at-rest representation.
func HashKey(k string) string {
	h := sha256.Sum256([]byte(k))
	return hex.EncodeToString(h[:])
}
