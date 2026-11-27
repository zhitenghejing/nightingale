package router

import (
	"context"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func (rt *Router) archiveWorkbook(c *gin.Context) {
	version, _ := strconv.ParseInt(c.Query("version_id"), 10, 64)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	q := workbook.Query{Operation: c.DefaultQuery("operation", "sheets"), Sheet: c.Query("sheet"), Range: c.Query("range"), Offset: archiveQueryInt(c, "offset"), Limit: archiveQueryInt(c, "limit")}
	// Archive preview only exposes catalog/read, never a general query executor.
	if q.Operation != "sheets" {
		q.Operation = "read"
	}
	r, e := rt.archiveService().Workbook(ctx, archiveInt(c, "document_id"), version, q)
	archiveResult(c, r, e)
}
