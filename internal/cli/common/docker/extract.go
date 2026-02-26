package docker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyNonEmptyContents recursively copies only non-empty files and directories from
// a Docker container extraction, skipping system directories that Docker creates.
func CopyNonEmptyContents(src, dst string) error {
	skipDirs := map[string]struct{}{
		"dev":  {},
		"etc":  {},
		"proc": {},
		"sys":  {},
	}
	return copyNonEmptyContentsWithSkip(src, dst, skipDirs)
}

func copyNonEmptyContentsWithSkip(src, dst string, skipDirs map[string]struct{}) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == ".dockerenv" {
			continue
		}
		if _, skip := skipDirs[name]; skip {
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if entry.IsDir() {
			if !hasNonEmptyContent(srcPath, skipDirs) {
				continue
			}
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return fmt.Errorf("create destination directory %s: %w", dstPath, err)
			}
			if err := copyNonEmptyContentsWithSkip(srcPath, dstPath, skipDirs); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat source file %s: %w", srcPath, err)
		}
		if info.Size() == 0 {
			continue
		}
		if err := CopyFile(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy file %s: %w", srcPath, err)
		}
	}
	return nil
}

func hasNonEmptyContent(dir string, skipDirs map[string]struct{}) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == ".dockerenv" {
			continue
		}
		if entry.IsDir() {
			if _, skip := skipDirs[name]; skip {
				continue
			}
			if hasNonEmptyContent(filepath.Join(dir, name), skipDirs) {
				return true
			}
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

// CopyFile copies a single file from src to dst, preserving permissions.
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
}
