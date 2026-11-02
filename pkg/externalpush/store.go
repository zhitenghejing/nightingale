package externalpush

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/poster"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Submission struct {
	ChannelID    int64                  `json:"channel_id"`
	NotifyRuleID int64                  `json:"notify_rule_id"`
	Params       map[string]interface{} `json:"params"`
	Event        *models.AlertCurEvent  `json:"event"`
}

func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

type DispatchSubmission struct {
	Routes []Submission `json:"routes"`
}

// Register is the single-route convenience entry point. Runtime fan-out uses
// RegisterDispatch with the entire applicable route set before any task is sent.
func Register(ctx *nctx.Context, in Submission) (int64, error) {
	ids, err := RegisterDispatch(ctx, DispatchSubmission{Routes: []Submission{in}})
	if len(ids) == 0 {
		return 0, err
	}
	return ids[0], err
}

func RegisterDispatch(ctx *nctx.Context, in DispatchSubmission) ([]int64, error) {
	if len(in.Routes) == 0 || len(in.Routes) > 1000 {
		return nil, errors.New("a complete dispatch plan with 1 to 1000 applicable routes is required")
	}
	if !ctx.IsCenter {
		return poster.PostByUrlsWithRespRetry[[]int64](ctx.WithCenterApiTimeout(2000), "/v1/n9e/external-push-tasks", in, 3, 100*time.Millisecond)
	}
	if in.Routes[0].Event == nil {
		return nil, errors.New("event is required")
	}
	dispatch := in.Routes[0].Event.DispatchID
	for _, route := range in.Routes {
		if route.Event == nil || route.Event.DispatchID != dispatch {
			return nil, errors.New("all routes must belong to one dispatch")
		}
	}
	sort.SliceStable(in.Routes, func(i, j int) bool {
		a, b := in.Routes[i], in.Routes[j]
		if a.ChannelID != b.ChannelID {
			return a.ChannelID < b.ChannelID
		}
		if a.NotifyRuleID != b.NotifyRuleID {
			return a.NotifyRuleID < b.NotifyRuleID
		}
		x, _ := json.Marshal(a.Params)
		y, _ := json.Marshal(b.Params)
		return string(x) < string(y)
	})
	type group struct {
		snapshot      Snapshot
		channel, rule int64
		conflict      bool
	}
	groups := map[string]*group{}
	endpointDestinations := map[string]string{}
	aliasedDestinations := map[string]bool{}
	var order []string
	for _, route := range in.Routes {
		ch, err := models.NotifyChannelGet(ctx, "id = ?", route.ChannelID)
		if err != nil {
			return nil, err
		}
		if ch == nil || !ch.Enable || ch.RequestType != models.ExternalPushType {
			return nil, errors.New("dedicated external push channel is missing or disabled")
		}
		if !validID(dispatch) {
			id, err := saveInvalidIdentity(models.DB(ctx), ch, route)
			return []int64{id}, err
		}
		var config models.ExternalPushConfig
		if ch.RequestConfig != nil && ch.RequestConfig.ExternalPushConfig != nil {
			config = *ch.RequestConfig.ExternalPushConfig
		}
		clean, sourceErr := canonicalEvent(models.DB(ctx), route.Event)
		if sourceErr != nil {
			clean = models.AlertCurEvent{Id: route.Event.Id, DispatchID: dispatch, IncidentID: route.Event.IncidentID, IncidentSequence: route.Event.IncidentSequence, IsRecovered: route.Event.IsRecovered}
		}
		snap := Snapshot{Config: config, Params: routeParams(route.Params), Event: clean, Routes: []RouteReference{{ChannelID: ch.ID, NotifyRuleID: route.NotifyRuleID}}}
		if sourceErr != nil {
			snap.InputError = sourceErr.Error()
		} else if err := routeConsensus(models.DB(ctx), snap, route.NotifyRuleID); err != nil {
			snap.InputError = err.Error()
		}
		key := config.Destination
		if key == "" {
			key = fmt.Sprintf("invalid-config-channel-%d", ch.ID)
		}
		if previous, exists := endpointDestinations[config.Endpoint]; exists && previous != key {
			aliasedDestinations[previous], aliasedDestinations[key] = true, true
		} else {
			endpointDestinations[config.Endpoint] = key
		}
		g := groups[key]
		if g == nil {
			groups[key] = &group{snapshot: snap, channel: ch.ID, rule: route.NotifyRuleID}
			order = append(order, key)
			continue
		}
		p, _ := Build(snap)
		original, _ := Build(g.snapshot)
		if protocolFingerprint(snap, p) != protocolFingerprint(g.snapshot, original) || snap.InputError != g.snapshot.InputError {
			g.conflict = true
		}
		g.snapshot.Routes = append(g.snapshot.Routes, snap.Routes...)
	}
	// All applicable routes have been inspected before the first INSERT can be
	// observed by a worker. No timeout-based debounce or first-route-wins window.
	var ids []int64
	var failures []error
	for _, key := range order {
		g := groups[key]
		if g.conflict {
			g.snapshot.InputError = "conflicting applicable routes for same dispatch and destination; no request sent"
		}
		if aliasedDestinations[key] {
			g.conflict = true
			g.snapshot.InputError = "same dispatch and endpoint use different destination keys; no request sent"
		}
		id, err := Enqueue(models.DB(ctx), g.snapshot, g.channel, g.rule)
		if id > 0 {
			ids = append(ids, id)
		}
		if err != nil {
			failures = append(failures, err)
		}
		if g.conflict {
			failures = append(failures, errors.New(g.snapshot.InputError))
		}
	}
	return ids, errors.Join(failures...)
}

func saveInvalidIdentity(db *gorm.DB, ch *models.NotifyChannelConfig, in Submission) (int64, error) {
	t := models.ExternalPushTask{DedupeKey: uuid.NewString(), StreamKey: uuid.NewString(), Destination: "invalid-identity", DispatchID: "", IncidentID: "", State: Invalid, Reason: "missing or invalid dispatch UUID; cannot deduplicate this input safely", Version: 1, ChannelID: ch.ID, NotifyRuleID: in.NotifyRuleID, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}
	err := db.Create(&t).Error
	return t.ID, err
}

// Enqueue commits an immutable snapshot under a database unique constraint.
// Rule/subscription IDs are provenance, not part of semantic deduplication.
func Enqueue(db *gorm.DB, s Snapshot, channelID, ruleID int64) (int64, error) {
	resolveSources(db, &s)
	raw, err := json.Marshal(s)
	if err != nil {
		return 0, err
	}
	if len(raw) > 32000 {
		return 0, errors.New("external push snapshot exceeds 32000 bytes")
	}
	now := time.Now().UnixMilli()
	destination := s.Config.Destination
	if destination == "" || len(destination) > 128 {
		destination = "invalid-config-channel-" + fmt.Sprint(channelID)
	}
	// Exclude fan-out provenance and mutable rendering metadata from the fingerprint.
	// Compare the actual protocol/configuration instead; divergent matches are held.
	p, validation := Build(s)
	payload, _ := json.Marshal(p)
	state, reason := Pending, "awaiting worker"
	if validation != nil {
		state, reason = Invalid, validation.Error()

	}
	kind := "problem"
	if s.Event.IsRecovered {
		kind = "recovery"
	}
	t := models.ExternalPushTask{DedupeKey: digest(destination + "\x00" + s.Event.DispatchID), StreamKey: digest(destination + "\x00" + s.Event.IncidentID), Destination: destination, DispatchID: s.Event.DispatchID, IncidentID: s.Event.IncidentID, Sequence: s.Event.IncidentSequence, Kind: kind, EventID: s.Event.Id, NotifyRuleID: ruleID, ChannelID: channelID, State: state, Reason: reason, Snapshot: string(raw), Fingerprint: protocolFingerprint(s, p), Payload: string(payload), Version: 1, CreatedAt: now, UpdatedAt: now}
	var conflict bool
	err = db.Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&t)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing models.ExternalPushTask
			if err := tx.Where("dedupe_key = ?", t.DedupeKey).First(&existing).Error; err != nil {
				return err
			}
			t.ID = existing.ID
			// Two first registrations can resolve mutable inventory before either
			// INSERT commits. Rebase on the winner's frozen sources before deciding
			// whether the immutable route/event inputs actually conflict.
			var winner Snapshot
			if json.Unmarshal([]byte(existing.Snapshot), &winner) == nil && reuseSources(&s, winner) {
				candidate, _ := Build(s)
				t.Fingerprint = protocolFingerprint(s, candidate)
			}
			// Old hashes included the retired reminder policy. Compare from the
			// immutable snapshot without rewriting its historical hash or state.
			existingFingerprint := existing.Fingerprint
			if json.Unmarshal([]byte(existing.Snapshot), &winner) == nil {
				original, _ := Build(winner)
				existingFingerprint = protocolFingerprint(winner, original)
			}
			if existingFingerprint != t.Fingerprint {
				conflict = true
				after := existing.State
				if existing.Attempts == 0 {
					r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND attempts = 0 AND state IN ?", existing.ID, []string{Pending, Waiting, Manual}).Updates(map[string]interface{}{"state": Invalid, "reason": "conflicting protocol mappings for same destination/dispatch; no request sent", "version": gorm.Expr("version + 1"), "updated_at": now})
					if r.Error != nil {
						return r.Error
					}
					if r.RowsAffected == 1 {
						after = Invalid
					}
				}
				op := models.ExternalPushOperation{TaskID: t.ID, Actor: "system", Action: "configuration_conflict", Reason: "same destination/dispatch matched divergent protocol snapshots; original snapshot retained", BeforeState: existing.State, AfterState: after, CreatedAt: now}
				if err := tx.Create(&op).Error; err != nil {
					return err
				}
			}
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.ExternalPushStream{Key: t.StreamKey}).Error
	})
	if err == nil && conflict {
		err = errors.New("same destination/dispatch matched conflicting configuration; inspect task audit")
	}
	return t.ID, err
}

type Detail struct {
	OmittedTags    []TagOmission                  `json:"omitted_tags"`
	PayloadPreview *Payload                       `json:"payload_preview,omitempty"`
	Task           models.ExternalPushTask        `json:"task"`
	Attempts       []models.ExternalPushAttempt   `json:"attempts"`
	Operations     []models.ExternalPushOperation `json:"operations"`
}

func Get(db *gorm.DB, id int64) (Detail, error) {
	d := Detail{OmittedTags: []TagOmission{}, Attempts: []models.ExternalPushAttempt{}, Operations: []models.ExternalPushOperation{}}
	if err := db.First(&d.Task, id).Error; err != nil {
		return d, err
	}
	// Admin detail is a read-only view of the stored payload, never a rebuild
	// from mutable rules. Signing material and the full snapshot stay private.
	var preview Payload
	if json.Unmarshal([]byte(d.Task.Payload), &preview) == nil {
		preview.Sign = "[REDACTED]"
		d.PayloadPreview = &preview
	}
	var snapshot Snapshot
	if json.Unmarshal([]byte(d.Task.Snapshot), &snapshot) == nil {
		if snapshot.Params["device_tags_source"] == "event_labels" {
			source := snapshot
			if d.Task.Kind == "recovery" && d.Task.Attempts > 0 {
				var original models.ExternalPushTask
				if err := db.Where("stream_key = ? AND kind = ? AND sequence < ? AND attempts > 0 AND state IN ?", d.Task.StreamKey, "problem", d.Task.Sequence, []string{Accepted, Confirmed}).Order("sequence").First(&original).Error; err != nil {
					return d, err
				}
				if err := json.Unmarshal([]byte(original.Snapshot), &source); err != nil {
					return d, err
				}
			}
			if d.Task.Kind != "recovery" || d.Task.Attempts > 0 {
				_, omitted, _ := joinedTagLabels(source)
				if omitted != nil {
					d.OmittedTags = omitted
				}
			}
		}
	}
	if err := db.Where("task_id = ?", id).Order("id").Find(&d.Attempts).Error; err != nil {
		return d, err
	}
	err := db.Where("task_id = ?", id).Order("id").Find(&d.Operations).Error
	return d, err
}

type Action struct {
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Version int64  `json:"version"`
}

func Act(db *gorm.DB, id int64, actor string, a Action) error {
	if strings.TrimSpace(a.Reason) == "" || len(a.Reason) > 1000 {
		return errors.New("a nonempty reason of at most 1000 bytes is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var t models.ExternalPushTask
		if err := tx.First(&t, id).Error; err != nil {
			return err
		}
		if t.Version != a.Version {
			return errors.New("task changed; refresh before making an explicit decision")
		}
		next := Manual
		switch a.Action {
		case "cancel_unsent":
			if t.Attempts != 0 || (t.State != Pending && t.State != Waiting && t.State != Manual && t.State != Invalid) {
				return errors.New("only an unattempted task can be cancelled; an unknown request must be reconciled")
			}
			next = NotDelivered
		case "confirm_received":
			if t.State != Unknown && t.State != Manual {
				return errors.New("only an unknown or manually held task can be confirmed received")
			}
			if t.Attempts == 0 {
				return errors.New("cannot confirm a task that has never been attempted")
			}
			next = Confirmed
		case "authorize_resend":
			if t.State != Unknown && t.State != Manual {
				return errors.New("only unknown or manually held tasks can be authorized")
			}
			var snap Snapshot
			if err := json.Unmarshal([]byte(t.Snapshot), &snap); err != nil {
				return err
			}
			if _, err := Build(snap); err != nil {
				return err
			}
			next = Pending
		case "hold":
			if t.State == Sending || t.State == Accepted || t.State == Confirmed || t.State == NotDelivered {
				return errors.New("cannot hold a sending, accepted or manually confirmed task")
			}
		default:
			return errors.New("unknown explicit action")
		}
		r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND version = ? AND state = ?", id, a.Version, t.State).Updates(map[string]interface{}{"state": next, "reason": "manual: " + a.Action, "version": t.Version + 1, "updated_at": time.Now().UnixMilli()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return errors.New("task changed; refresh before making an explicit decision")
		}
		return tx.Create(&models.ExternalPushOperation{TaskID: id, Actor: actor, Action: a.Action, Reason: a.Reason, BeforeState: t.State, AfterState: next, CreatedAt: time.Now().UnixMilli()}).Error
	})
}
