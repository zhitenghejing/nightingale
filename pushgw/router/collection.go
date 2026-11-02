package router

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/prometheus/prompb"
)

func hasCollectionLabels(labels []prompb.Label) bool {
	for _, l := range labels {
		if strings.HasPrefix(l.Name, "collection_") || l.Name == "probe_id" {
			return true
		}
	}
	return false
}
func (rt *Router) collectionSampleAllowed(c *gin.Context, series *prompb.TimeSeries) bool {
	if rt.Collection == nil {
		return true
	}
	if hasCollectionLabels(series.Labels) {
		return false
	}
	user, _, _ := c.Request.BasicAuth()
	out, err := rt.Collection.FilterSamples(user, []prompb.TimeSeries{*series})
	return err == nil && len(out) > 0
}
