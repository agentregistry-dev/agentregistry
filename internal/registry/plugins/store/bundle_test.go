package store

import (
	"archive/tar"
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func sampleBundle() *CanonicalBundle {
	return &CanonicalBundle{Files: map[string][]byte{
		"SKILL.md":               []byte("---\nname: x\n---\nbody\n"),
		"skills/deploy/SKILL.md": []byte("---\nname: deploy\n---\n"),
		".mcp.json":              []byte(`{"mcpServers":{"db":{}}}`),
		"bin/tool":               []byte("#!/bin/sh\n"),
	}}
}

func TestBundleBytesDeterministic(t *testing.T) {
	a := sampleBundle()
	b := sampleBundle()
	ab, err := a.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	bb, err := b.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ab, bb) {
		t.Fatal("two bundles with identical content produced different tar bytes")
	}
	h1, _ := a.ContentHash()
	h2, _ := b.ContentHash()
	if h1 != h2 || h1 == "" {
		t.Fatalf("content hash unstable: %q vs %q", h1, h2)
	}
}

func TestCanonicalizeIdempotent(t *testing.T) {
	raw, err := sampleBundle().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	b2, err := Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := b2.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, raw2) {
		t.Fatal("Canonicalize(b.Bytes()).Bytes() != b.Bytes() — canonicalization not idempotent")
	}
}

func TestFromTarRoundTrip(t *testing.T) {
	orig := sampleBundle()
	raw, err := orig.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromTar(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(orig.Files, got.Files) {
		t.Fatalf("round-trip mismatch:\n want %v\n got  %v", orig.Files, got.Files)
	}
}

func TestBytesRejectsNonUSTARPath(t *testing.T) {
	// A 120-byte single-segment name cannot be represented in USTAR (no '/'
	// split point); with Format forced to USTAR, WriteHeader must fail rather
	// than silently emit PAX.
	long := strings.Repeat("a", 120)
	b := &CanonicalBundle{Files: map[string][]byte{long: []byte("x")}}
	_, err := b.Bytes()
	if !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected ErrInvalidBundle for non-USTAR path, got %v", err)
	}
}

func TestBundlePathTraversalRejected(t *testing.T) {
	for _, p := range []string{"../evil", "/abs", "a/../../b", "a\\b"} {
		b := &CanonicalBundle{Files: map[string][]byte{p: []byte("x")}}
		if _, err := b.Bytes(); !errors.Is(err, ErrInvalidBundle) {
			t.Fatalf("path %q: expected ErrInvalidBundle, got %v", p, err)
		}
	}
}

func TestFromTarRejectsTraversal(t *testing.T) {
	// Craft a tar (GNU format, which permits "..") and confirm FromTar rejects it.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape", Size: 1, Mode: 0o644, Typeflag: tar.TypeReg, Format: tar.FormatGNU})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	if _, err := FromTar(buf.Bytes()); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected ErrInvalidBundle from traversal tar, got %v", err)
	}
}
