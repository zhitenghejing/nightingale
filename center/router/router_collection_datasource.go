package router

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/gin-gonic/gin"
)

func collectionUIUser(c *gin.Context) (*models.User, bool) {
	principal, _ := c.Get("user")
	user, ok := principal.(*models.User)
	return user, ok && user != nil
}

// A nil set means no UI datasource filter is installed. Native service Target
// reads use their existing service authorization, rather than a UI principal.
func (rt *Router) collectionDatasourcePermissions(c *gin.Context) map[int64]bool {
	principal, exists := collectionUIUser(c)
	if !exists || rt.DatasourceCache == nil || rt.DatasourceCache.DatasourceFilter == nil {
		return nil
	}
	allowed := map[int64]bool{}
	sources := rt.DatasourceCache.GetByCate("prometheus")
	for _, source := range rt.DatasourceCache.DatasourceFilter(sources, principal) {
		allowed[source.Id] = true
	}
	return allowed
}

// Apply permissions before accessing the shared status cache, which may already
// contain values fetched by a privileged reader or the background arming worker.
func (rt *Router) collectionStatesForUser(c *gin.Context, devices []models.MonitorDevice) (map[string]collection.State, error) {
	allowed := rt.collectionDatasourcePermissions(c)
	if allowed == nil || len(devices) == 0 {
		return rt.CollectionStatus.States(c.Request.Context(), devices)
	}
	ids := []string{}
	for _, device := range devices {
		ids = append(ids, device.CollectorID)
	}
	var collectors []models.MonitorCollector
	if err := rt.Collection.DB.Where("id IN ?", ids).Find(&collectors).Error; err != nil {
		return nil, err
	}
	byID := map[string]models.MonitorCollector{}
	for _, collector := range collectors {
		byID[collector.ID] = collector
	}
	visible := []models.MonitorDevice{}
	out := map[string]collection.State{}
	for _, device := range devices {
		collector := byID[device.CollectorID]
		if allowed[collector.DatasourceID] {
			visible = append(visible, device)
		} else {
			out[device.ID] = collection.DatasourceUnavailableState(device, collector)
		}
	}
	states, err := rt.CollectionStatus.States(c.Request.Context(), visible)
	for id, state := range states {
		out[id] = state
	}
	return out, err
}
