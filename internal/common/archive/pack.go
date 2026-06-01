package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	FormatTarGz = "tar.gz"
	FormatZip   = "zip"
)

// PackResult is the outcome of PackDirAuto (pure Go; no external tar/zip commands).
type PackResult struct {
	ArchivePath string // empty when Skipped
	Format      string // tar.gz or zip when packed
	Skipped     bool   // true when all formats failed or caller used --no-pack
	Message     string // user-facing hint (fallback or skip reason)
}

// PackDirAuto tries tar.gz first, then zip. If both fail, Skipped is true and Message explains why.
// Archives are written as siblings: <srcDir>.tar.gz or <srcDir>.zip.
func PackDirAuto(srcDir string) (PackResult, error) {
	srcDir = filepath.Clean(srcDir)
	info, err := os.Stat(srcDir)
	if err != nil {
		return PackResult{}, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return PackResult{}, fmt.Errorf("source is not a directory: %s", srcDir)
	}

	tarPath, zipPath := siblingArchivePaths(srcDir)

	if err := packTarGz(srcDir, filepath.Base(srcDir), tarPath); err == nil {
		return PackResult{ArchivePath: tarPath, Format: FormatTarGz}, nil
	} else {
		tarErr := err
		if err := packZip(srcDir, filepath.Base(srcDir), zipPath); err == nil {
			return PackResult{
				ArchivePath: zipPath,
				Format:      FormatZip,
				Message:     fmt.Sprintf("tar.gz failed (%v); created zip instead: %s", tarErr, zipPath),
			}, nil
		}
		return PackResult{
			Skipped: true,
			Message: fmt.Sprintf(
				"archive skipped: tar.gz failed (%v); zip failed (%v); results remain in %s",
				tarErr, err, srcDir,
			),
		}, nil
	}
}

func siblingArchivePaths(srcDir string) (tarGz, zipPath string) {
	base := filepath.Base(srcDir)
	parent := filepath.Dir(srcDir)
	return filepath.Join(parent, base+".tar.gz"), filepath.Join(parent, base+".zip")
}

func packTarGz(srcDir, tarBaseName, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if sameOrSubPath(path, destPath) {
			if path == destPath {
				return nil
			}
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)

		fi, err := d.Info()
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join(tarBaseName, rel))

		if d.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("walk for tar.gz: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func packZip(srcDir, zipBaseName, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if sameOrSubPath(path, destPath) {
			if path == destPath {
				return nil
			}
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(filepath.Join(zipBaseName, rel))

		if d.IsDir() {
			name += "/"
			_, err := zw.Create(name)
			return err
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}

		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("walk for zip: %w", err)
	}
	return zw.Close()
}

// NotifyPackOutcome prints a non-fatal pack message to stderr (terminal).
func NotifyPackOutcome(message string) {
	if message == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "Warning: %s\n", message)
}

func sameOrSubPath(path, parent string) bool {
	path = filepath.Clean(path)
	parent = filepath.Clean(parent)
	if path == parent {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(path, parent+sep)
}
