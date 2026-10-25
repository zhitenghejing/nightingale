package router

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ccfos/nightingale/v6/archives"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
)

func (rt *Router) archiveService() *archives.Service {
	rt.archivesOnce.Do(func() {
		rt.archivesService = archives.New(rt.Ctx.DB, rt.Center.Archives.DataDir)
		c := rt.Center.Archives
		rt.archivesService.ConfigureExports(c.NodePath, c.LibreOfficePath, c.ExportTimeoutSeconds)
		rt.archivesService.StartExports()
	})
	return rt.archivesService
}
func archiveActor(c *gin.Context) archives.Actor {
	u := c.MustGet("user").(*models.User)
	name := u.Nickname
	if name == "" {
		name = u.Username
	}
	return archives.Actor{ID: u.Id, Username: u.Username, Name: name}
}
func archiveInt(c *gin.Context, name string) int64 {
	v, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || v <= 0 {
		ginx.Bomb(http.StatusBadRequest, "Invalid archive identifier")
	}
	return v
}
func archiveQueryInt(c *gin.Context, name string) int { v, _ := strconv.Atoi(c.Query(name)); return v }
func archiveResult(c *gin.Context, value interface{}, err error) {
	if err == nil {
		ginx.NewRender(c).Data(value, nil)
		return
	}
	var p *archives.Problem
	if errors.As(err, &p) {
		message := i18nx.Translate(c.GetHeader("X-Language"), p.Message)
		c.JSON(p.Status, gin.H{"err": message, "error": gin.H{"code": p.Code, "message": message, "current": p.Current}})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"err": i18nx.Translate(c.GetHeader("X-Language"), "Archive or document not found")})
		return
	}
	logger.Errorf("archive operation: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"err": i18nx.Translate(c.GetHeader("X-Language"), "Archive operation failed")})
}
func archiveBind(c *gin.Context, value interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12*1024*1024)
	if err := c.ShouldBindJSON(value); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"err": i18nx.Translate(c.GetHeader("X-Language"), "Invalid archive request")})
		return false
	}
	return true
}
func (rt *Router) configArchiveRoutes(pages *gin.RouterGroup) {
	g := pages.Group("/archives", rt.auth(), rt.user())
	g.POST("/:id/exports", rt.archiveExportCreate)
	g.GET("/exports/:export_id", rt.archiveExportStatus)
	g.GET("/exports/:export_id/file", rt.archiveExportFile)
	g.POST("/:id/print", rt.archivePrintLog)
	g.GET("/licenses", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "private, no-store")
		c.Writer.Write(archives.RendererNotices)
	})
	g.GET("/limits", func(c *gin.Context) {
		archiveResult(c, gin.H{"file_bytes": archives.MaxFileBytes, "image_bytes": archives.MaxImageBytes, "body_bytes": archives.MaxBodyBytes, "archive_bytes": archives.MaxArchiveBytes, "documents": archives.MaxDocuments}, nil)
	})
	g.GET("", rt.archiveList)
	g.POST("", rt.archiveCreate)
	g.DELETE("", rt.archiveBatchDelete)
	g.GET("/files/:file_id", rt.archiveFile)
	g.GET("/documents/:document_id", rt.archiveDocument)
	g.GET("/documents/:document_id/versions", rt.archiveDocumentVersions)
	g.POST("/documents/:document_id/versions/:version_id/restore", rt.archiveDocumentRestore)
	g.POST("/documents/:document_id/lease", rt.archiveAcquire)
	g.POST("/documents/:document_id/lease/resume", rt.archiveResume)
	g.PUT("/documents/:document_id/lease", rt.archiveHeartbeat)
	g.DELETE("/documents/:document_id/lease", rt.archiveRelease)
	g.PUT("/documents/:document_id/draft", rt.archiveDraft)
	g.POST("/documents/:document_id/draft/discard", rt.archiveDraftDiscard)
	g.POST("/documents/:document_id/save", rt.archiveSave)
	g.GET("/documents/:document_id/bookmark", rt.archiveBookmarkGet)
	g.PUT("/documents/:document_id/bookmark", rt.archiveBookmarkPut)
	g.GET("/:id", rt.archiveGet)
	g.PUT("/:id", rt.archiveUpdate)
	g.DELETE("/:id", rt.archiveDelete)
	g.POST("/:id/archive", rt.archiveFinalize)
	g.POST("/:id/restore", rt.archiveRestore)
	g.POST("/:id/documents", rt.archiveCreateDocument)
	g.PUT("/:id/documents/:document_id", rt.archiveUpdateDocument)
	g.DELETE("/:id/documents/:document_id", rt.archiveDeleteDocument)
	g.POST("/:id/files", rt.archiveUpload)
	g.POST("/:id/assets", rt.archiveUploadAsset)
	g.GET("/:id/versions", rt.archiveVersions)
	g.POST("/:id/versions/:version_id/restore", rt.archiveVersionRestore)
	g.GET("/:id/logs", rt.archiveLogs)
}
func (rt *Router) archiveList(c *gin.Context) {
	q := archives.ListQuery{Category: c.Query("category"), Keyword: c.Query("q"), State: c.Query("state"), Department: c.Query("department"), Location: c.Query("location"), Result: c.Query("result"), Level: c.Query("level"), Start: c.Query("start"), End: c.Query("end"), Deleted: c.Query("deleted") == "true", Page: archiveQueryInt(c, "page"), Limit: archiveQueryInt(c, "limit")}
	v, err := rt.archiveService().List(q)
	archiveResult(c, v, err)
}
func (rt *Router) archiveCreate(c *gin.Context) {
	var in struct {
		Category string            `json:"category"`
		Metadata archives.Metadata `json:"metadata"`
	}
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().Create(archiveActor(c), in.Category, in.Metadata)
	archiveResult(c, v, err)
}
func (rt *Router) archiveGet(c *gin.Context) {
	v, err := rt.archiveService().Get(archiveInt(c, "id"))
	archiveResult(c, v, err)
}
func (rt *Router) archiveUpdate(c *gin.Context) {
	var in struct {
		Revision          int64             `json:"revision"`
		Metadata          archives.Metadata `json:"metadata"`
		PrimaryDocumentID int64             `json:"primary_document_id"`
	}
	if !archiveBind(c, &in) {
		return
	}
	if in.Revision <= 0 {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "revision_required", Message: "Archive revision is required"})
		return
	}
	v, err := rt.archiveService().Update(archiveActor(c), archiveInt(c, "id"), in.Revision, in.Metadata, in.PrimaryDocumentID)
	archiveResult(c, v, err)
}
func archiveRevision(c *gin.Context) (int64, bool) {
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !archiveBind(c, &in) {
		return 0, false
	}
	if in.Revision <= 0 {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "revision_required", Message: "Archive revision is required"})
		return 0, false
	}
	return in.Revision, true
}
func (rt *Router) archiveFinalize(c *gin.Context) {
	rev, ok := archiveRevision(c)
	if !ok {
		return
	}
	v, err := rt.archiveService().Archive(archiveActor(c), archiveInt(c, "id"), rev)
	archiveResult(c, v, err)
}
func (rt *Router) archiveDelete(c *gin.Context) {
	rev, ok := archiveRevision(c)
	if !ok {
		return
	}
	archiveResult(c, nil, rt.archiveService().Delete(archiveActor(c), archiveInt(c, "id"), rev))
}
func (rt *Router) archiveRestore(c *gin.Context) {
	rev, ok := archiveRevision(c)
	if !ok {
		return
	}
	v, err := rt.archiveService().Restore(archiveActor(c), archiveInt(c, "id"), rev)
	archiveResult(c, v, err)
}
func (rt *Router) archiveBatchDelete(c *gin.Context) {
	var in struct {
		Items []struct {
			ID       int64 `json:"id"`
			Revision int64 `json:"revision"`
		} `json:"items"`
	}
	if !archiveBind(c, &in) {
		return
	}
	if len(in.Items) < 1 || len(in.Items) > 100 {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "invalid_selection", Message: "Select between 1 and 100 archives"})
		return
	}
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].ID < in.Items[j].ID })
	for i, item := range in.Items {
		if item.ID <= 0 || item.Revision <= 0 || (i > 0 && item.ID == in.Items[i-1].ID) {
			archiveResult(c, nil, &archives.Problem{Status: 422, Code: "invalid_selection", Message: "Invalid archive selection"})
			return
		}
	}
	svc := rt.archiveService()
	err := svc.DB.Transaction(func(tx *gorm.DB) error {
		inner := *svc
		inner.DB = tx
		for _, item := range in.Items {
			if err := inner.Delete(archiveActor(c), item.ID, item.Revision); err != nil {
				return err
			}
		}
		return nil
	})
	archiveResult(c, nil, err)
}
func (rt *Router) archiveCreateDocument(c *gin.Context) {
	var in archives.CreateDocument
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().CreateOnline(archiveActor(c), archiveInt(c, "id"), in)
	archiveResult(c, v, err)
}
func (rt *Router) archiveDeleteDocument(c *gin.Context) {
	archiveResult(c, nil, rt.archiveService().DeleteDocument(archiveActor(c), archiveInt(c, "id"), archiveInt(c, "document_id")))
}
func (rt *Router) archiveUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, archives.MaxFileBytes+1024*1024)
	f, header, err := c.Request.FormFile("file")
	if err != nil {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "invalid_file", Message: "Upload is missing or exceeds 50 MiB"})
		return
	}
	defer f.Close()
	documentID, _ := strconv.ParseInt(c.Request.FormValue("document_id"), 10, 64)
	epoch, _ := strconv.ParseInt(c.Request.FormValue("epoch"), 10, 64)
	v, err := rt.archiveService().Upload(archiveActor(c), archiveInt(c, "id"), documentID, header.Filename, c.Request.FormValue("purpose"), c.Request.FormValue("session_id"), epoch, f)
	archiveResult(c, v, err)
}
func (rt *Router) archiveUploadAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, archives.MaxImageBytes+1024*1024)
	f, header, err := c.Request.FormFile("file")
	if err != nil {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "invalid_image", Message: "Image is missing or exceeds 10 MiB"})
		return
	}
	defer f.Close()
	documentID, _ := strconv.ParseInt(c.Request.FormValue("document_id"), 10, 64)
	v, err := rt.archiveService().UploadAsset(archiveActor(c), archiveInt(c, "id"), documentID, header.Filename, f, archives.EditInput{SessionID: c.Request.FormValue("session_id"), Epoch: int64(archiveFormInt(c, "epoch"))})
	archiveResult(c, v, err)
}
func (rt *Router) archiveFile(c *gin.Context) {
	svc := rt.archiveService()
	b, file, err := svc.Blob(archiveInt(c, "file_id"))
	if err != nil {
		archiveResult(c, nil, err)
		return
	}
	if b.Asset && b.Staged && b.CreateBy != archiveActor(c).ID {
		archiveResult(c, nil, &archives.Problem{Status: 403, Code: "private_draft", Message: "This image belongs to an unpublished editing draft"})
		return
	}
	f, err := os.Open(file)
	if err != nil {
		archiveResult(c, nil, err)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		archiveResult(c, nil, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", b.ContentType)
	if c.Query("download") == "true" {
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+strings.ReplaceAll(url.QueryEscape(b.Name), "+", "%20"))
		if err := svc.Audit(archiveActor(c), b.ArchiveID, b.DocumentID, "download_original", gin.H{"blob_id": b.ID}); err != nil {
			archiveResult(c, nil, err)
			return
		}
	}
	http.ServeContent(c.Writer, c.Request, b.Name, stat.ModTime(), f)
}
func (rt *Router) archiveDocument(c *gin.Context) {
	version, _ := strconv.ParseInt(c.Query("version_id"), 10, 64)
	v, err := rt.archiveService().Document(archiveInt(c, "document_id"), version)
	archiveResult(c, v, err)
}
func (rt *Router) archiveAcquire(c *gin.Context) {
	v, err := rt.archiveService().Acquire(archiveActor(c), archiveInt(c, "document_id"))
	archiveResult(c, v, err)
}
func (rt *Router) archiveHeartbeat(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().Heartbeat(archiveActor(c), archiveInt(c, "document_id"), in.SessionID, in.Epoch)
	archiveResult(c, v, err)
}
func (rt *Router) archiveRelease(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	archiveResult(c, nil, rt.archiveService().Release(archiveActor(c), archiveInt(c, "document_id"), in.SessionID, in.Epoch))
}
func (rt *Router) archiveDraft(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	archiveResult(c, nil, rt.archiveService().Draft(archiveActor(c), archiveInt(c, "document_id"), in))
}
func (rt *Router) archiveSave(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().Save(archiveActor(c), archiveInt(c, "document_id"), in)
	archiveResult(c, v, err)
}
func (rt *Router) archiveVersions(c *gin.Context) {
	rows := []models.ArchiveVersion{}
	db := rt.archiveService().DB
	offset := archiveQueryInt(c, "offset")
	if offset < 0 {
		offset = 0
	}
	var total int64
	q := db.Model(&models.ArchiveVersion{}).Where("archive_id = ?", archiveInt(c, "id"))
	err := q.Count(&total).Error
	if err == nil {
		err = q.Order("sequence desc").Offset(offset).Limit(100).Find(&rows).Error
	}
	archiveResult(c, gin.H{"list": rows, "total": total}, err)
}
func (rt *Router) archiveDocumentVersions(c *gin.Context) {
	rows := []models.ArchiveDocumentVersion{}
	q := rt.archiveService().DB.Model(&models.ArchiveDocumentVersion{}).Where("document_id = ?", archiveInt(c, "document_id"))
	offset := archiveQueryInt(c, "offset")
	if offset < 0 {
		offset = 0
	}
	var total int64
	err := q.Count(&total).Error
	if err == nil {
		err = q.Omit("body").Order("sequence desc").Offset(offset).Limit(100).Find(&rows).Error
	}
	archiveResult(c, gin.H{"list": rows, "total": total}, err)
}
func (rt *Router) archiveDocumentRestore(c *gin.Context) {
	v, err := rt.archiveService().RestoreDocument(archiveActor(c), archiveInt(c, "document_id"), archiveInt(c, "version_id"))
	archiveResult(c, v, err)
}
func (rt *Router) archiveVersionRestore(c *gin.Context) {
	rev, ok := archiveRevision(c)
	if !ok {
		return
	}
	v, err := rt.archiveService().RestoreArchive(archiveActor(c), archiveInt(c, "id"), archiveInt(c, "version_id"), rev)
	archiveResult(c, v, err)
}
func (rt *Router) archiveLogs(c *gin.Context) {
	v, err := rt.archiveService().Logs(archiveInt(c, "id"), archiveQueryInt(c, "offset"))
	archiveResult(c, v, err)
}
func (rt *Router) archiveBookmarkGet(c *gin.Context) {
	v, err := rt.archiveService().GetBookmark(archiveActor(c), archiveInt(c, "document_id"))
	archiveResult(c, v, err)
}
func (rt *Router) archiveBookmarkPut(c *gin.Context) {
	var in struct {
		Position  models.ArchiveJSON `json:"position"`
		VersionID int64              `json:"version_id"`
	}
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().Bookmark(archiveActor(c), archiveInt(c, "document_id"), in.VersionID, in.Position)
	archiveResult(c, v, err)
}

func (rt *Router) archiveExportCreate(c *gin.Context) {
	var in struct {
		DocumentID int64  `json:"document_id"`
		VersionID  int64  `json:"version_id"`
		Format     string `json:"format"`
		ForReading bool   `json:"for_reading"`
	}
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().CreateExport(archiveActor(c), archiveInt(c, "id"), in.DocumentID, in.VersionID, in.Format, in.ForReading)
	archiveResult(c, v, err)
}
func (rt *Router) archiveExportStatus(c *gin.Context) {
	v, _, err := rt.archiveService().Export(c.Param("export_id"))
	if v != nil {
		v.Error = i18nx.Translate(c.GetHeader("X-Language"), v.Error)
	}
	archiveResult(c, v, err)
}
func (rt *Router) archiveExportFile(c *gin.Context) {
	svc := rt.archiveService()
	v, p, err := svc.Export(c.Param("export_id"))
	if err != nil {
		archiveResult(c, nil, err)
		return
	}
	if v.State != "done" {
		archiveResult(c, nil, &archives.Problem{Status: 409, Code: "export_pending", Message: "Export is not ready"})
		return
	}
	inline := c.Query("inline") == "true" && (v.Format == "pdf" || v.Format == "annotated_pdf" || v.Format == "information_pdf")
	if !inline {
		if err := svc.Audit(archiveActor(c), v.ArchiveID, v.DocumentID, "download_export", gin.H{"job": v.ID, "format": v.Format}); err != nil {
			archiveResult(c, nil, err)
			return
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if inline {
		c.Header("Content-Type", "application/pdf")
		c.File(p)
	} else {
		c.FileAttachment(p, v.FileName)
	}
}
func (rt *Router) archivePrintLog(c *gin.Context) {
	var in struct {
		DocumentID int64 `json:"document_id"`
		VersionID  int64 `json:"version_id"`
	}
	if !archiveBind(c, &in) {
		return
	}
	archiveResult(c, nil, rt.archiveService().Audit(archiveActor(c), archiveInt(c, "id"), in.DocumentID, "print_requested", gin.H{"version_id": in.VersionID}))
}

func archiveFormInt(c *gin.Context, key string) int {
	n, _ := strconv.Atoi(c.Request.FormValue(key))
	return n
}
func (rt *Router) archiveDraftDiscard(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	archiveResult(c, nil, rt.archiveService().DiscardDraft(archiveActor(c), archiveInt(c, "document_id"), in))
}

func (rt *Router) archiveUpdateDocument(c *gin.Context) {
	var in struct {
		Revision int64  `json:"revision"`
		Title    string `json:"title"`
		Purpose  string `json:"purpose"`
	}
	if !archiveBind(c, &in) {
		return
	}
	if in.Revision <= 0 {
		archiveResult(c, nil, &archives.Problem{Status: 422, Code: "revision_required", Message: "Archive revision is required"})
		return
	}
	v, err := rt.archiveService().UpdateDocument(archiveActor(c), archiveInt(c, "id"), archiveInt(c, "document_id"), in.Revision, in.Title, in.Purpose)
	archiveResult(c, v, err)
}

func (rt *Router) archiveResume(c *gin.Context) {
	var in archives.EditInput
	if !archiveBind(c, &in) {
		return
	}
	v, err := rt.archiveService().Resume(archiveActor(c), archiveInt(c, "document_id"), in)
	archiveResult(c, v, err)
}
