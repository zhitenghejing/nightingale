package cmdb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reportFields() Report {
	return Report{Kind: "inspection", Title: "Monthly inspection", Engineer: "Engineer A", ReportDate: "2026-10-21", Summary: "All systems checked"}
}
func docxFixture(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Inspection report preview test</w:t></w:r></w:p><w:sectPr/></w:body></w:document>`,
	}
	for name, data := range files {
		f, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write([]byte(data)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestReportsPersistenceOwnershipAndVersion(t *testing.T) {
	root := t.TempDir()
	s, e := NewReportStore(root)
	if e != nil {
		t.Fatal(e)
	}
	original := []byte("%PDF-1.4\nreport bytes\n%%EOF")
	owner := User{ID: 7}
	r, e := s.Create(owner, reportFields(), "../../report.pdf", bytes.NewReader(original))
	if e != nil {
		t.Fatal(e)
	}
	if r.Filename != "report.pdf" || r.PreviewStatus != "ready" || r.OwnerID != 7 || len(r.SHA256) != 64 {
		t.Fatal(r)
	}
	reopened, e := NewReportStore(root)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := reopened.List(owner, "inspection")
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if rows, e = reopened.List(User{ID: 8}, ""); e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e = reopened.Get(User{ID: 8}, r.ID); e == nil {
		t.Fatal("other user read report")
	}
	if rows, e = reopened.List(User{Admin: true}, ""); e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	fields := r
	fields.Summary = "Edited summary"
	updated, e := reopened.Update(owner, r.ID, fields)
	if e != nil || updated.Version != 2 {
		t.Fatal(updated, e)
	}
	if _, e = reopened.Update(owner, r.ID, fields); e == nil {
		t.Fatal("stale update accepted")
	}
	got, e := os.ReadFile(filepath.Join(root, r.ID, "original.pdf"))
	if e != nil || !bytes.Equal(original, got) {
		t.Fatal("original changed", e)
	}
	revisions, _ := filepath.Glob(filepath.Join(root, r.ID, "revision-*.json"))
	if len(revisions) != 2 {
		t.Fatal(revisions)
	}
	if _, e = reopened.Get(owner, "../../etc/passwd"); e == nil {
		t.Fatal("path traversal")
	}
}
func TestReportsRejectInvalidFilesAndOversize(t *testing.T) {
	s, _ := NewReportStore(t.TempDir())
	for _, f := range []string{"report.exe", "report.pdf", "report.doc", "report.docx"} {
		if _, e := s.Create(User{ID: 1}, reportFields(), f, strings.NewReader("not a valid document")); e == nil {
			t.Fatal("accepted", f)
		}
	}
	if _, e := s.Create(User{ID: 1}, reportFields(), "large.pdf", io.MultiReader(strings.NewReader("%PDF-1.4"), io.LimitReader(zeroReader{}, reportFileLimit))); e == nil {
		t.Fatal("oversize accepted")
	}
	entries, _ := os.ReadDir(s.Root)
	if len(entries) != 0 {
		t.Fatal("failed upload leaked", entries)
	}
	if _, e := s.Create(User{ID: 1}, reportFields(), "valid.docx", bytes.NewReader(docxFixture(t))); e != nil {
		t.Fatal(e)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func TestReportsAuthenticatedUploadAndPreview(t *testing.T) {
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := 1
		if r.Header.Get("Authorization") == "Bearer other-user" {
			id = 2
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"dat": User{ID: int64(id)}, "err": ""})
	}))
	defer identity.Close()
	storage, _ := NewReportStore(t.TempDir())
	h := NewHandlerWithReports(nil, identity.URL, storage)
	call := func(method, path, token string, b io.Reader, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/cmdb/reports"+path, b)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "", "", nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	var b bytes.Buffer
	mp := multipart.NewWriter(&b)
	meta, _ := json.Marshal(reportFields())
	_ = mp.WriteField("metadata", string(meta))
	file, _ := mp.CreateFormFile("file", "report.pdf")
	_, _ = file.Write([]byte("%PDF-1.4\ncontent\n%%EOF"))
	_ = mp.Close()
	w := call("POST", "", "owner-user", &b, mp.FormDataContentType())
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Dat Report `json:"dat"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	id := response.Dat.ID
	if w = call("GET", "/"+id+"/preview", "other-user", nil, ""); w.Code != 404 {
		t.Fatal("unauthorized preview", w.Code)
	}
	if w = call("GET", "/"+id+"/preview", "owner-user", nil, ""); w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || !strings.HasPrefix(w.Body.String(), "%PDF-") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/"+id+"/file", "owner-user", nil, ""); !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(w.Header())
	}
}
func TestReportWordConversionIsolated(t *testing.T) {
	if os.Getenv("CMDB_REPORT_CONVERSION_TEST") != "1" {
		t.Skip("opt in to isolated LibreOffice integration")
	}
	s, _ := NewReportStore(t.TempDir())
	u := User{ID: 1}
	source := docxFixture(t)
	v, e := s.Create(u, reportFields(), "test.docx", bytes.NewReader(source))
	if e != nil {
		t.Fatal(e)
	}
	s.convert(context.Background(), v)
	result, e := s.Get(u, v.ID)
	if e != nil || result.PreviewStatus != "ready" {
		t.Fatal(result, e)
	}
	path := filepath.Join(s.Root, v.ID, "preview.pdf")
	if e = validateReportFile(path, ".pdf"); e != nil {
		t.Fatal(e)
	}
	original, e := os.ReadFile(filepath.Join(s.Root, v.ID, "original.docx"))
	if e != nil || !bytes.Equal(original, source) {
		t.Fatal("original modified", e)
	}
}
