// Package id generates short, URL-safe, non-enumerable page identifiers.
package id

import (
	"crypto/rand"
	"fmt"
)

const (
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// Length 12 over base62 gives ~71 bits of entropy.
	Length = 12
)

// New returns a random base62 id of Length characters.
func New() (string, error) {
	buf := make([]byte, Length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	out := make([]byte, Length)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
