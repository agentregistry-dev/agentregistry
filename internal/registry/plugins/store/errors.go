// Package store persists canonical plugin bundles as content-addressed OCI
// artifacts and reads them back. The canonical bundle (see CanonicalBundle) is
// the portable core of a plugin — a flat, deterministically-serialized set of
// files — that is hashed, pushed, and later materialized into a harness's
// on-disk layout. This package is pure storage: it does not touch the database
// and does not decide where bundle bytes originate (that is the ingest seam
// wired at the composition root).
package store

import "errors"

var (
	// ErrInvalidBundle is returned when bundle content cannot be represented
	// canonically (path traversal, non-USTAR-representable path, malformed tar).
	ErrInvalidBundle = errors.New("invalid plugin bundle")
	// ErrNotDigestPinned is returned by Pull when the reference is not pinned
	// to a digest (…@sha256:…).
	ErrNotDigestPinned = errors.New("oci reference must be digest-pinned")
	// ErrPush wraps registry write failures.
	ErrPush = errors.New("push plugin bundle failed")
	// ErrPull wraps registry read failures.
	ErrPull = errors.New("pull plugin bundle failed")
)
