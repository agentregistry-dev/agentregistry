// Package testutil provides shared helpers for the registries package tests.
package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateRandomPackageName returns a random, almost-certainly-nonexistent
// package name, useful for exercising "package not found" code paths in tests.
func GenerateRandomPackageName() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a static name if crypto/rand fails
		return "nonexistent-pkg-fallback"
	}
	return fmt.Sprintf("nonexistent-pkg-%s", hex.EncodeToString(bytes))
}
