package cmdb

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const reportFileLimit = 30 << 20

type Report struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Title         string    `json:"title"`
	Engineer      string    `json:"engineer"`
	Company       string    `json:"company"`
	Contact       string    `json:"contact"`
	ReportDate    string    `json:"reportDate"`
	Summary       string    `json:"summary"`
	Filename      string    `json:"filename"`
	Extension     string    `json:"extension"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	OwnerID       int64     `json:"ownerId"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Version       int       `json:"version"`
	PreviewStatus string    `json:"previewStatus"`
	PreviewError  string    `json:"previewError,omitempty"`
}

// Each report is an atomic directory containing its original, metadata revisions
// and derived preview. This directory must be on persistent, backed-up storage.
type ReportStore struct {
	Root string
	mu   sync.Mutex
}

func NewReportStore(root string) (*ReportStore, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("报告存储目录必须是绝对路径")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &ReportStore{Root: root}, nil
}
func validReportID(id string) bool { _, e := uuid.Parse(id); return e == nil && len(id) == 36 }
func (s *ReportStore) read(id string) (Report, error) {
	var v Report
	if !validReportID(id) {
		return v, os.ErrNotExist
	}
	b, e := os.ReadFile(filepath.Join(s.Root, id, "metadata.json"))
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v)
	return v, e
}
func (s *ReportStore) List(u User, kind string) ([]Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, e := os.ReadDir(s.Root)
	if e != nil {
		return nil, e
	}
	out := []Report{}
	for _, f := range entries {
		if !f.IsDir() || !validReportID(f.Name()) {
			continue
		}
		v, e := s.read(f.Name())
		if e != nil {
			return nil, errors.New("报告索引损坏，请联系管理员检查备份")
		}
		if (u.Admin || u.ID == v.OwnerID) && (kind == "" || v.Kind == kind) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (s *ReportStore) Get(u User, id string) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.read(id)
	if e != nil || (!u.Admin && u.ID != v.OwnerID) {
		return Report{}, os.ErrNotExist
	}
	return v, nil
}
func validateReport(v *Report) error {
	v.Title = strings.TrimSpace(v.Title)
	v.Engineer = strings.TrimSpace(v.Engineer)
	if (v.Kind != "inspection" && v.Kind != "incident") || v.Title == "" || v.Engineer == "" || strings.TrimSpace(v.Summary) == "" {
		return errors.New("报告类别、标题、工程师及摘要必填")
	}
	if len(v.Title) > 600 || len(v.Engineer) > 240 || len(v.Company) > 600 || len(v.Contact) > 240 || len(v.Summary) > 15000 {
		return errors.New("报告信息超过长度限制")
	}
	if _, e := time.Parse("2006-01-02", v.ReportDate); e != nil {
		return errors.New("请选择有效的报告日期")
	}
	return nil
}
func syncFile(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func writeReport(dir string, v Report) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	// Keep every metadata revision, including engineer and summary edits.
	revision := filepath.Join(dir, fmt.Sprintf("revision-%06d.json", v.Version))
	if e = syncFile(revision, b); e != nil {
		return e
	}
	tmp, e := os.CreateTemp(dir, ".metadata-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.Write(b); e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(dir, "metadata.json")); e != nil {
		return e
	}
	return syncDir(dir)
}
func validateReportFile(path, ext string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	head := make([]byte, 8)
	_, e = io.ReadFull(f, head)
	if e != nil {
		return errors.New("附件内容不完整")
	}
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return errors.New("附件不是有效 PDF 文件")
		}
	case ".doc":
		if !bytes.Equal(head, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
			return errors.New("附件不是有效 Word DOC 文件")
		}
	case ".docx":
		z, e := zip.OpenReader(path)
		if e != nil {
			return errors.New("附件不是有效 Word DOCX 文件")
		}
		defer z.Close()
		document, content := false, false
		var total uint64
		for _, f := range z.File {
			total += f.UncompressedSize64
			if total > 256<<20 || len(z.File) > 10000 {
				return errors.New("Word 解压后体积过大")
			}
			document = document || f.Name == "word/document.xml"
			content = content || f.Name == "[Content_Types].xml"
			if strings.Contains(strings.ToLower(f.Name), "vbaproject") {
				return errors.New("不支持含宏的 Word 文档")
			}
		}
		if !document || !content {
			return errors.New("DOCX 缺少文档内容")
		}
	default:
		return errors.New("仅支持 PDF、DOC、DOCX 附件")
	}
	return nil
}
func (s *ReportStore) Create(u User, v Report, filename string, src io.Reader) (Report, error) {
	if e := validateReport(&v); e != nil {
		return Report{}, e
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".pdf" && ext != ".doc" && ext != ".docx" {
		return Report{}, errors.New("仅支持 PDF、DOC、DOCX 附件")
	}
	if len(filename) > 600 {
		return Report{}, errors.New("附件文件名过长")
	}
	dir, e := os.MkdirTemp(s.Root, ".upload-")
	if e != nil {
		return Report{}, e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "original"+ext)
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return Report{}, e
	}
	hash := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, hash), io.LimitReader(src, reportFileLimit+1))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return Report{}, e
	}
	if n > reportFileLimit {
		return Report{}, errors.New("附件不得超过 30 MB")
	}
	if e = validateReportFile(path, ext); e != nil {
		return Report{}, e
	}
	now := time.Now().UTC()
	v.ID = uuid.NewString()
	v.Filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
	v.Extension = ext
	v.Size = n
	v.SHA256 = hex.EncodeToString(hash.Sum(nil))
	v.OwnerID = u.ID
	v.CreatedAt = now
	v.UpdatedAt = now
	v.Version = 1
	v.PreviewStatus = "pending"
	v.PreviewError = ""
	if ext == ".pdf" {
		v.PreviewStatus = "ready"
	}
	if e = writeReport(dir, v); e != nil {
		return Report{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = os.Rename(dir, filepath.Join(s.Root, v.ID)); e != nil {
		return Report{}, e
	}
	if e = syncDir(s.Root); e != nil {
		return Report{}, e
	}
	return v, nil
}
func (s *ReportStore) Update(u User, id string, fields Report) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, e := s.read(id)
	if e != nil || (!u.Admin && old.OwnerID != u.ID) {
		return Report{}, os.ErrNotExist
	}
	if fields.Version != old.Version {
		return Report{}, errors.New("报告已更新，请刷新后重试")
	}
	next := old
	next.Title = fields.Title
	next.Engineer = fields.Engineer
	next.Contact = fields.Contact
	next.Company = fields.Company
	next.ReportDate = fields.ReportDate
	next.Summary = fields.Summary
	if e = validateReport(&next); e != nil {
		return Report{}, e
	}
	next.Version++
	next.UpdatedAt = time.Now().UTC()
	return next, writeReport(filepath.Join(s.Root, id), next)
}

// One conversion at a time; a private, networkless namespace exposes only the
// current report and system libraries. A restart retries pending reports.
func (s *ReportStore) Run(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			rows, e := s.List(User{Admin: true}, "")
			if e != nil {
				continue
			}
			for _, v := range rows {
				if ctx.Err() != nil {
					return
				}
				if v.PreviewStatus == "pending" {
					s.convert(ctx, v)
				}
			}
		}
	}
}
func (s *ReportStore) convert(ctx context.Context, v Report) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	dir := filepath.Join(s.Root, v.ID)
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8"}
	for _, p := range []string{"/usr", "/bin", "/lib", "/lib64", "/etc/fonts", "/etc/libreoffice", "/etc/ld.so.cache", "/etc/localtime"} {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		args = append(args, "--ro-bind", p, p)
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--ro-bind", dir, "/input", "--dir", "/output")
	// Converter cannot modify originals, metadata or other reports.
	output, e := os.MkdirTemp(s.Root, ".convert-")
	if e != nil {
		return
	}
	defer os.RemoveAll(output)
	args = append(args, "--bind", output, "/output", "--setenv", "HOME", "/tmp", "--chdir", "/output", "/usr/bin/prlimit", "--as=2147483648", "--cpu=40", "--fsize=104857600", "--", "/usr/bin/libreoffice", "-env:UserInstallation=file:///tmp/lo-profile", "--headless", "--nologo", "--nodefault", "--norestore", "--convert-to", "pdf:writer_pdf_Export", "--outdir", "/output", "/input/original"+v.Extension)
	e = exec.CommandContext(ctx, "bwrap", args...).Run()
	if parent.Err() != nil {
		return
	}
	preview := filepath.Join(output, "original.pdf")
	if e == nil {
		e = validateReportFile(preview, ".pdf")
	}
	if e == nil {
		e = os.Chmod(preview, 0600)
	}
	if e == nil {
		f, err := os.OpenFile(preview, os.O_RDWR, 0600)
		if err != nil {
			e = err
		} else {
			e = f.Sync()
			f.Close()
		}
	}
	if e == nil {
		e = os.Rename(preview, filepath.Join(dir, "preview.pdf"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.read(v.ID)
	if err != nil {
		return
	}
	current.Version++
	current.UpdatedAt = time.Now().UTC()
	if e != nil {
		current.PreviewStatus = "failed"
		current.PreviewError = "Word 预览转换失败，可下载原件；请检查文档是否加密、损坏或转换服务是否可用"
	} else {
		current.PreviewStatus = "ready"
		current.PreviewError = ""
	}
	_ = writeReport(dir, current)
}
func NewHandlerWithReports(s *Store, origin string, reports *ReportStore) http.Handler {
	a := NewHandler(s, origin).(*API)
	a.Reports = reports
	return a
}
func (a *API) reportHTTP(w http.ResponseWriter, r *http.Request, u User) {
	if a.Reports == nil {
		respond(w, r, 503, nil, errors.New("报告存储未配置"))
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/cmdb/reports"), "/")
	if path == "" && r.Method == "GET" {
		rows, e := a.Reports.List(u, r.URL.Query().Get("kind"))
		if e != nil {
			respond(w, r, 500, nil, e)
		} else {
			respond(w, r, 200, rows, nil)
		}
		return
	}
	if path == "" && r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, reportFileLimit+(1<<20))
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			respond(w, r, 400, nil, errors.New("上传内容无效或超过 30 MB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		var v Report
		if e := json.Unmarshal([]byte(r.FormValue("metadata")), &v); e != nil {
			respond(w, r, 400, nil, errors.New("报告信息格式错误"))
			return
		}
		file, header, e := r.FormFile("file")
		if e != nil {
			respond(w, r, 400, nil, errors.New("请选择报告附件"))
			return
		}
		defer file.Close()
		saved, e := a.Reports.Create(u, v, header.Filename, file)
		if e != nil {
			respond(w, r, 400, nil, e)
		} else {
			respond(w, r, 201, saved, nil)
		}
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	v, e := a.Reports.Get(u, id)
	if e != nil {
		respond(w, r, 404, nil, errors.New("报告不存在或无权访问"))
		return
	}
	if len(parts) == 1 && r.Method == "PUT" {
		var fields Report
		if e = body(w, r, &fields); e == nil {
			v, e = a.Reports.Update(u, id, fields)
		}
		if e != nil {
			respond(w, r, 400, nil, e)
		} else {
			respond(w, r, 200, v, nil)
		}
		return
	}
	if len(parts) == 2 && r.Method == "GET" && (parts[1] == "file" || parts[1] == "preview") {
		filename := "original" + v.Extension
		mimeType := map[string]string{".pdf": "application/pdf", ".doc": "application/msword", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}[v.Extension]
		disposition := "attachment"
		if parts[1] == "preview" {
			if v.PreviewStatus != "ready" {
				respond(w, r, 409, nil, errors.New("预览暂未就绪"))
				return
			}
			mimeType = "application/pdf"
			disposition = "inline"
			if v.Extension != ".pdf" {
				filename = "preview.pdf"
			}
		}
		file, e := os.Open(filepath.Join(a.Reports.Root, id, filename))
		if e != nil {
			respond(w, r, 404, nil, errors.New("附件缺失，请联系管理员检查备份"))
			return
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil {
			respond(w, r, 500, nil, e)
			return
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": v.Filename}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Security-Policy", "sandbox")
		http.ServeContent(w, r, filename, info.ModTime(), file)
		return
	}
	respond(w, r, 405, nil, errors.New("报告接口或请求方法不存在"))
}
