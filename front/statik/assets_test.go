package statik_test

import (
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ccfos/nightingale/v6/front/statik"
	statikfs "github.com/rakyll/statik/fs"
)

func TestEmbeddedFrontend(t *testing.T) {
	embedded, err := statikfs.New()
	if err != nil {
		t.Fatal(err)
	}
	index, err := embedded.Open("/index.html")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(index)
	index.Close()
	if err != nil || !strings.Contains(string(data), `id="root"`) {
		t.Fatalf("embedded index is not a frontend document: %v", err)
	}
	// Release verification additionally compares every file to the built assets.
	// Plain Go checkouts still verify their committed bundle without Node output.
	directory := os.Getenv("N9E_TEST_FRONTEND_DIR")
	if directory == "" {
		return
	}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := embedded.Open("/" + filepath.ToSlash(name))
		if err != nil {
			return err
		}
		got, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return err
		}
		if sha256.Sum256(want) != sha256.Sum256(got) {
			t.Errorf("embedded asset differs from production output: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
