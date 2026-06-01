package archive

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackDirAutoTarGz(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "collect", "20260101-120000")
	if err := os.MkdirAll(filepath.Join(src, "hosts", "10.0.0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := PackDirAuto(src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.ArchivePath == "" {
		t.Fatalf("expected archive, got %+v", res)
	}
	if res.Format != FormatTarGz {
		t.Fatalf("format: %s", res.Format)
	}
	arc := res.ArchivePath
	if _, err := os.Stat(arc); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	f, err := os.Open(arc)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	if len(names) == 0 {
		t.Fatal("empty tar")
	}
	found := false
	for _, n := range names {
		if n == "20260101-120000/manifest.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("manifest not in tar: %v", names)
	}
}

func TestDefaultOutputDirUnderCwd(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	dir, _, err := DefaultOutputDir("collect")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(dir), "/output/collect/") {
		t.Fatalf("unexpected dir %s", dir)
	}
}
