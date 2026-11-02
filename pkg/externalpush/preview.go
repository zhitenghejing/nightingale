package externalpush

import (
	"errors"
	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

type PreviewInput struct {
	ChannelID int64                  `json:"channel_id"`
	EventID   int64                  `json:"event_id"`
	Params    map[string]interface{} `json:"params"`
}
type PreviewResult struct {
	Payload         *Payload      `json:"payload"`
	OmittedTags     []TagOmission `json:"omitted_tags"`
	RecoveryPending bool          `json:"recovery_pending"`
}

// Preview only reads saved event/channel records and evaluates draft parameters.
// It never registers tasks, executes pipelines, signs, or sends network requests.
func Preview(db *gorm.DB, in PreviewInput) (PreviewResult, error) {
	result := PreviewResult{OmittedTags: []TagOmission{}}
	if in.EventID <= 0 || in.ChannelID <= 0 {
		return result, errors.New("channel_id and historical event_id are required")
	}
	var ch models.NotifyChannelConfig
	if err := db.First(&ch, in.ChannelID).Error; err != nil {
		return result, errors.New("selected saved channel is missing")
	}
	if ch.RequestType != models.ExternalPushType || ch.RequestConfig == nil || ch.RequestConfig.ExternalPushConfig == nil {
		return result, errors.New("selected channel is not a dedicated external push channel")
	}
	var historical models.AlertHisEvent
	if err := db.First(&historical, in.EventID).Error; err != nil {
		return result, errors.New("selected historical event is missing")
	}
	event := historical.ToCur()
	clean, err := canonicalEvent(db, event)
	if err != nil {
		return result, err
	}
	snap := Snapshot{Config: *ch.RequestConfig.ExternalPushConfig, Params: routeParams(in.Params), Event: clean}
	resolveSources(db, &snap)
	payload, err := Build(snap)
	if err != nil {
		return result, err
	}
	if !snap.Event.IsRecovered && snap.Params["device_tags_source"] == "event_labels" {
		_, result.OmittedTags, err = joinedTagLabels(snap)
		if err != nil {
			return result, err
		}
	}
	result.RecoveryPending = snap.Event.IsRecovered
	payload.Sign = "[REDACTED]"
	result.Payload = &payload
	return result, nil
}
