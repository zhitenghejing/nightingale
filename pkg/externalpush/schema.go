package externalpush

import (
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

// CheckSchema verifies required ledger and event identity schema before center
// starts any services, including when the dedicated channel is disabled.
func CheckSchema(db *gorm.DB) error {
	for _, model := range []interface{}{&models.ExternalPushTask{}, &models.ExternalPushStream{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}} {
		if !db.Migrator().HasTable(model) {
			return fmt.Errorf("external push ledger migration incomplete: missing %T", model)
		}
	}
	if !db.Migrator().HasIndex(&models.ExternalPushTask{}, "ux_ep_task_dedupe") {
		return fmt.Errorf("external push task unique index is missing")
	}
	for _, event := range []interface{}{&models.AlertCurEvent{}, &models.AlertHisEvent{}} {
		for _, column := range []string{"DispatchID", "IncidentID", "IncidentSequence"} {
			if !db.Migrator().HasColumn(event, column) {
				return fmt.Errorf("external push event identity migration incomplete: %T.%s", event, column)
			}
		}
	}
	return nil
}
