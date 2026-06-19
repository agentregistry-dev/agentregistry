package store

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

// CanonicalBundle is the portable core of a plugin: a flat, path-keyed set of
// files (SKILL.md, AGENTS.md, .mcp.json, hooks/*, commands/*, agents/*, bin/*).
// It is the unit that is hashed, pushed, and pulled. It is NOT harness-specific;
// translation to a harness's on-disk layout happens at pull/deploy time.
//
// Paths are clean, relative, forward-slash separated (no leading "/" or "..").
type CanonicalBundle struct {
	Files map[string][]byte
}

// Bytes returns the deterministic tar encoding of the bundle. Identical content
// yields byte-identical output regardless of map iteration order, host OS, or
// build time, so the content hash and the pushed layer are stable:
//   - entries sorted by path
//   - header Mode 0644, regular files only, all timestamps zeroed, Uid/Gid 0,
//     Uname/Gname empty
//   - tar.FormatUSTAR forced: a path that USTAR cannot represent makes
//     WriteHeader fail (returned as ErrInvalidBundle) rather than silently
//     falling back to PAX (which would embed nondeterministic records)
//   - uncompressed (the layer media type is an honest ".tar")
func (b *CanonicalBundle) Bytes() ([]byte, error) {
	paths := make([]string, 0, len(b.Files))
	for p := range b.Files {
		if err := validateBundlePath(p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// USTAR only carries mtime (no atime/ctime). Set ModTime to a stable epoch
	// and leave AccessTime/ChangeTime as the zero value so the header stays
	// USTAR-encodable and deterministic.
	epoch := time.Unix(0, 0).UTC()
	for _, p := range paths {
		data := b.Files[p]
		hdr := &tar.Header{
			Name:     p,
			Size:     int64(len(data)),
			Mode:     0o644,
			Typeflag: tar.TypeReg,
			ModTime:  epoch,
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("%w: path %q is not USTAR-representable: %v", ErrInvalidBundle, p, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FromTar reconstructs a CanonicalBundle from tar bytes. Directory and
// non-regular entries are skipped; every regular-file path is traversal-checked.
// FromTar does not itself canonicalize ordering/headers — call Bytes() to
// re-serialize canonically (Canonicalize composes the two).
func FromTar(tarBytes []byte) (*CanonicalBundle, error) {
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	files := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if err := validateBundlePath(hdr.Name); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("%w: reading %q: %v", ErrInvalidBundle, hdr.Name, err)
		}
		files[hdr.Name] = data
	}
	return &CanonicalBundle{Files: files}, nil
}

// Canonicalize parses raw tar bytes (which may be non-canonical, e.g. a client
// tar) into a CanonicalBundle. The result is canonical-by-construction: calling
// Bytes() on it always produces the normalized form, so
// Canonicalize(x).Bytes() == Canonicalize(x).Bytes() for equal logical content.
func Canonicalize(rawTar []byte) (*CanonicalBundle, error) {
	return FromTar(rawTar)
}

// ContentHash is sha256(Bytes()) in hex — the canonical bundle digest written
// to spec.Content.ContentHash. It is distinct from the store's spec
// content_hash column (which hashes the resource's JSON, not the bundle bytes).
func (b *CanonicalBundle) ContentHash() (string, error) {
	raw, err := b.Bytes()
	if err != nil {
		return "", err
	}
	return sha256hex(raw), nil
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// validateBundlePath rejects empty, absolute, non-clean, backslash, and
// parent-traversal paths.
func validateBundlePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrInvalidBundle)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("%w: backslash in path %q", ErrInvalidBundle, p)
	}
	if path.IsAbs(p) {
		return fmt.Errorf("%w: absolute path %q", ErrInvalidBundle, p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("%w: non-clean path %q", ErrInvalidBundle, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: parent traversal in path %q", ErrInvalidBundle, p)
		}
	}
	return nil
}
