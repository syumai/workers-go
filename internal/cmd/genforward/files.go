package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// resetOutDir makes dir exist and removes everything under it except a
// top-level .git directory, since genforward owns the whole tree it
// generates into.
func resetOutDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// copyStaticFiles copies the files the mirror needs that genforward doesn't
// itself generate:
//   - <srcDir>/LICENSE.md -> <outDir>/LICENSE.md
//   - everything under staticDir -> <outDir>/ (verbatim), if staticDir is
//     non-empty. staticDir is where other tooling places static mirror-only
//     files such as README.md and the cmd/workers-assets-gen stub (see
//     internal/cmd/genforward/_static). Since staticDir is an explicit
//     opt-in (typically the -static flag), it is an error for it to be
//     non-empty but not exist, or to exist and not be a directory; that
//     signals a misconfiguration rather than something to skip silently.
func copyStaticFiles(srcDir, staticDir, outDir string) error {
	licenseSrc := filepath.Join(srcDir, "LICENSE.md")
	if _, err := os.Stat(licenseSrc); err == nil {
		if err := copyFile(licenseSrc, filepath.Join(outDir, "LICENSE.md")); err != nil {
			return fmt.Errorf("copying LICENSE.md: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if staticDir == "" {
		return nil
	}

	info, err := os.Stat(staticDir)
	if err != nil {
		return fmt.Errorf("-static %s: %w", staticDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("-static %s: not a directory", staticDir)
	}

	return filepath.WalkDir(staticDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(staticDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		return copyFile(path, dst)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
