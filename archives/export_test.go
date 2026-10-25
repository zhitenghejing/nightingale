package archives

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
)

func TestArchiveRendererAndAnnotationExports(t *testing.T) {
	if os.Getenv("N9E_TEST_ARCHIVE_RENDERER") != "1" {
		t.Skip("set N9E_TEST_ARCHIVE_RENDERER=1 with Node 24, LibreOffice and Poppler")
	}
	s := newTestService(t)
	s.ConfigureExports(os.Getenv("N9E_ARCHIVE_NODE"), "", 120)
	a, id := other(t, s)
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	original := body("归档中文测试 Text integrity 123")
	var snapshot map[string]interface{}
	json.Unmarshal([]byte(original), &snapshot)
	snapshot["options"].(map[string]interface{})["defaultFont"] = "Noto Sans CJK SC"
	original = encode(snapshot)
	v := models.ArchiveDocumentVersion{Body: original}
	docxPath := filepath.Join(work, "result.docx")
	if err := s.renderVersion(ctx, work, docxPath, v, "docx", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	found := false
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			found = strings.Contains(string(b), "归档中文测试 Text integrity 123")
		}
	}
	if !found {
		t.Fatal("DOCX dropped Chinese/text")
	}
	pdfPath := filepath.Join(work, "result.pdf")
	if err := s.renderVersion(ctx, work, pdfPath, v, "pdf", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", pdfPath, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text, []byte("归档中文测试")) || !bytes.Contains(text, []byte("Text integrity")) {
		t.Fatalf("PDF text missing: %s", text)
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.Upload(alice, a.ID, 0, "report.pdf", "other", "", 0, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pdfDoc := a.Documents[len(a.Documents)-1]
	l, err := s.Acquire(alice, pdfDoc.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := edit(l.Lease, encode(AnnotationBody{Annotations: []Annotation{{ID: "test-note", Type: "note", Page: 1, Rects: []Rect{{X: 0.1, Y: 0.1, Width: 0.1, Height: 0.1}}, Text: "中文批注"}}}))
	saved, err := s.Save(alice, pdfDoc.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	annotated := filepath.Join(work, "annotated.pdf")
	if err := s.renderVersion(ctx, work, annotated, saved.Version, "annotated_pdf", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, mustRead(t, annotated)) {
		t.Fatal("annotations did not change export")
	}
	if !bytes.Equal(data, mustRead(t, filepath.Join(s.Root, "objects", mustBlob(t, s, pdfDoc.File.ID).StorageKey))) {
		t.Fatal("PDF original changed")
	}
	input.BaseVersionID = saved.CurrentVersionID
	input.Body = encode(AnnotationBody{Annotations: []Annotation{{Type: "note", Page: 999, Rects: []Rect{{X: 0.1, Y: 0.1, Width: 0.1, Height: 0.1}}}}})
	_, err = s.Save(alice, pdfDoc.ID, input)
	problemStatus(t, err, 422)
	s.Release(alice, pdfDoc.ID, l.Lease.SessionID, l.Lease.Epoch)
	// The selected historical version determines the export kind even when its
	// current attachment has subsequently been replaced by a different format.
	l, err = s.Acquire(alice, pdfDoc.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Upload(alice, a.ID, pdfDoc.ID, "replacement.png", "other", l.Lease.SessionID, l.Lease.Epoch, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	s.Release(alice, pdfDoc.ID, l.Lease.SessionID, l.Lease.Epoch)
	historyJob, err := s.CreateExport(alice, a.ID, pdfDoc.ID, saved.Version.ID, "annotated_pdf")
	if err != nil {
		t.Fatal("historical PDF export used the current file kind", err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		job, path, err := s.Export(historyJob.ID)
		if err != nil || job.State == "error" || time.Now().After(deadline) {
			t.Fatal("historical PDF export failed", err, job)
		}
		if job.State == "done" {
			if !bytes.HasPrefix(mustRead(t, path), []byte("%PDF-")) {
				t.Fatal("historical export did not produce PDF")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Full archive ZIP pins a manifest and includes originals + true online outputs.
	current, _ := s.Get(a.ID)
	infoPath := filepath.Join(work, "information.pdf")
	if err := s.renderInformation(ctx, work, infoPath, Snapshot{Record: current.ArchiveRecord}, s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	info, err := exec.Command("pdftotext", infoPath, "-").Output()
	if err != nil || !bytes.Contains(info, []byte("档案版本")) {
		t.Fatalf("information PDF: %v, %s", err, info)
	}
	job, err := s.CreateExport(alice, a.ID, 0, 0, "zip")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(80 * time.Second)
	for time.Now().Before(deadline) {
		current, p, err := s.Export(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "error" {
			t.Fatal(current.Error)
		}
		if current.State == "done" {
			z, err := zip.OpenReader(p)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			names := map[string]bool{}
			for _, f := range z.File {
				names[f.Name] = true
			}
			if !names["archive-information.pdf"] || !names["archive-information.json"] {
				t.Fatal(names)
			}
			if len(names) < 5 {
				t.Fatalf("incomplete archive: %v", names)
			}
			_ = id
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("export timed out")
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func mustBlob(t *testing.T, s *Service, id int64) *models.ArchiveBlob {
	t.Helper()
	b, _, e := s.Blob(id)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
