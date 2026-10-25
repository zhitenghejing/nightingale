package archives

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveEmbeddedRendererIdentity(t *testing.T) {
	var manifest struct {
		SHA256       string            `json:"sha256"`
		Inputs       map[string]string `json:"inputs"`
		NoticeSHA256 string            `json:"noticeSha256"`
	}
	b, err := os.ReadFile("renderer/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(renderWorker)
	if hex.EncodeToString(hash[:]) != manifest.SHA256 {
		t.Fatal("embedded renderer does not match manifest")
	}
	for p, want := range manifest.Inputs {
		b, err := os.ReadFile(filepath.Join("..", "frontend", p))
		if err != nil {
			t.Fatal(err)
		}
		got := sha256.Sum256(b)
		if hex.EncodeToString(got[:]) != want {
			t.Fatalf("renderer source changed: %s; run pnpm archive:worker", p)
		}
	}
	noticeHash := sha256.Sum256(RendererNotices)
	if hex.EncodeToString(noticeHash[:]) != manifest.NoticeSHA256 {
		t.Fatal("embedded third-party notices do not match manifest")
	}
	if len(RendererNotices) < 2000 {
		t.Fatal("third-party notices missing")
	}
}
