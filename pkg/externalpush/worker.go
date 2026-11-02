package externalpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
)

const leaseMS int64 = 30000

// Worker is center-owned. Step is shared by the runtime and simulation tests;
// every request uses a fresh client with no proxy, redirects or retries.
type Worker struct {
	DB   *gorm.DB
	Keys *FileKeys
}

func Start(ctx context.Context, db *gorm.DB) func() {
	return StartWithKeys(ctx, db, NewFileKeys(nil))
}

func StartWithKeys(ctx context.Context, db *gorm.DB, keys *FileKeys) func() {
	run, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	// Concurrency is bounded. Database stream leases arbitrate across workers
	// and across center instances, rather than an in-process mutex.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			w := Worker{DB: db, Keys: keys}
			for {
				select {
				case <-run.Done():
					return
				case <-ticker.C:
					if err := w.Step(run); err != nil && run.Err() == nil {
						logger.Errorf("external push worker: %v", err)
					}
				}
			}
		}()
	}
	return func() { cancel(); wg.Wait() }
}

// Expire does not requeue ambiguous attempts. Expired sending includes a crash
// after the receiver accepted but before the local acknowledgement committed.
func (w Worker) Expire(now int64) error {
	var tasks []models.ExternalPushTask
	if err := w.DB.Where("state = ? AND lease_until <= ?", Sending, now).Find(&tasks).Error; err != nil {
		return err
	}
	for _, t := range tasks {
		err := w.DB.Transaction(func(tx *gorm.DB) error {
			r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND state = ? AND lease_token = ? AND lease_until <= ?", t.ID, Sending, t.LeaseToken, now).Updates(map[string]interface{}{"state": Unknown, "reason": "worker lease expired; check receiver before any resend", "version": gorm.Expr("version + 1"), "updated_at": now})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected == 0 {
				return nil
			}
			if err := tx.Model(&models.ExternalPushAttempt{}).Where("task_id = ? AND token = ? AND state = ?", t.ID, t.LeaseToken, Sending).Updates(map[string]interface{}{"state": Unknown, "summary": "worker lease expired; delivery may have occurred", "finished_at": now}).Error; err != nil {
				return err
			}
			return tx.Create(&models.ExternalPushOperation{TaskID: t.ID, Actor: "system", Action: "lease_expired", Reason: "no automatic resend", BeforeState: Sending, AfterState: Unknown, CreatedAt: now}).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w Worker) Step(ctx context.Context) error {
	if err := w.Expire(time.Now().UnixMilli()); err != nil {
		return err
	}
	var tasks []models.ExternalPushTask
	if err := w.DB.Where("state IN ?", []string{Pending, Waiting}).Order("updated_at").Order("id").Limit(100).Find(&tasks).Error; err != nil {
		return err
	}
	for _, candidate := range tasks {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task, attempt, err := w.claim(candidate.ID)
		if err != nil {
			return err
		}
		if task == nil {
			continue
		}
		state, summary := send(ctx, *task)
		err = w.DB.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UnixMilli()
			r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND state = ? AND lease_token = ?", task.ID, Sending, attempt.Token).Updates(map[string]interface{}{"state": state, "reason": summary, "version": gorm.Expr("version + 1"), "updated_at": now, "lease_until": 0})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return errors.New("attempt no longer owns task; receiver result requires reconciliation")
			}
			if err := tx.Model(&models.ExternalPushAttempt{}).Where("id = ? AND state = ?", attempt.ID, Sending).Updates(map[string]interface{}{"state": state, "summary": summary, "finished_at": now}).Error; err != nil {
				return err
			}
			return tx.Model(&models.ExternalPushStream{}).Where("stream_key = ? AND lease_token = ?", task.StreamKey, attempt.Token).Updates(map[string]interface{}{"lease_until": 0, "lease_token": ""}).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (w Worker) claim(id int64) (*models.ExternalPushTask, *models.ExternalPushAttempt, error) {
	var claimed *models.ExternalPushTask
	var attempt *models.ExternalPushAttempt
	err := w.DB.Transaction(func(tx *gorm.DB) error {
		var t models.ExternalPushTask
		if err := tx.First(&t, id).Error; err != nil {
			return err
		}
		if t.State != Pending && t.State != Waiting {
			return nil
		}
		now := time.Now().UnixMilli()
		token := uuid.NewString()
		r := tx.Model(&models.ExternalPushStream{}).Where("stream_key = ? AND lease_until <= ?", t.StreamKey, now).Updates(map[string]interface{}{"lease_token": token, "lease_until": now + leaseMS})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return nil
		}
		release := func() error {
			return tx.Model(&models.ExternalPushStream{}).Where("stream_key = ? AND lease_token = ?", t.StreamKey, token).Updates(map[string]interface{}{"lease_until": 0, "lease_token": ""}).Error
		}
		setState := func(state, reason string) error {
			r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND version = ? AND state IN ?", id, t.Version, []string{Pending, Waiting}).Updates(map[string]interface{}{"state": state, "reason": reason, "version": t.Version + 1, "updated_at": now})
			if r.Error != nil {
				return r.Error
			}
			return release()
		}
		suppress := func(action, reason string) error {
			r := tx.Model(&models.ExternalPushTask{}).Where("id = ? AND version = ? AND state IN ?", id, t.Version, []string{Pending, Waiting}).Updates(map[string]interface{}{"state": NotDelivered, "reason": reason, "version": t.Version + 1, "updated_at": now})
			if r.Error != nil {
				return r.Error
			}
			if err := release(); err != nil {
				return err
			}
			if r.RowsAffected != 1 {
				return nil
			}
			return tx.Create(&models.ExternalPushOperation{TaskID: id, Actor: "system", Action: action, Reason: reason, BeforeState: t.State, AfterState: NotDelivered, CreatedAt: now}).Error
		}
		if t.ChannelID > 0 {
			var ch models.NotifyChannelConfig
			if err := tx.First(&ch, t.ChannelID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return setState(Manual, "channel was removed; snapshot retained")
				}
				return err
			}
			if !ch.Enable || ch.RequestType != models.ExternalPushType {
				return setState(Manual, "dedicated channel is disabled; no request sent")
			}
		}
		var s Snapshot
		if err := json.Unmarshal([]byte(t.Snapshot), &s); err != nil {
			return setState(Invalid, "invalid persisted snapshot")
		}
		if _, err := Build(s); err != nil {
			return setState(Invalid, err.Error())
		}
		if t.ChannelID > 0 {
			if err := routeConsensus(tx, s, t.NotifyRuleID); err != nil {
				return setState(Invalid, err.Error())
			}
		}

		// Source sequences include muted/filtered snapshots. Only real registered
		// deliveries are dependencies; never infer tasks for missing source numbers.
		var sameSequence int64
		if err := tx.Model(&models.ExternalPushTask{}).Where("stream_key = ? AND sequence = ? AND id <> ?", t.StreamKey, t.Sequence, t.ID).Count(&sameSequence).Error; err != nil {
			return err
		}
		if sameSequence > 0 {
			return setState(Manual, "different dispatches claim the same incident sequence; reconcile source identity")
		}
		var problems []models.ExternalPushTask
		if err := tx.Where("stream_key = ? AND kind = ? AND id <> ?", t.StreamKey, "problem", t.ID).Order("sequence").Order("id").Find(&problems).Error; err != nil {
			return err
		}
		var anchor *models.ExternalPushTask
		for i := range problems {
			p := &problems[i]
			if p.Sequence > t.Sequence && p.Attempts > 0 {
				return setState(Manual, "a later source problem was already attempted; late dispatch requires manual disposition")
			}
			if p.Sequence >= t.Sequence {
				continue
			}
			if p.Attempts == 0 {
				if t.Kind == "problem" && (p.State == Pending || p.State == Waiting) {
					return setState(Waiting, "earlier registered problem is pending; source sequence gaps are allowed")
				}
				continue
			}
			if p.State != Accepted && p.State != Confirmed {
				return setState(Waiting, "attempted problem has an unconfirmed result; reconcile before recovery or further delivery")
			}
			if anchor == nil {
				anchor = p
			}
		}
		if t.Kind == "problem" {
			var recoveries int64
			if err := tx.Model(&models.ExternalPushTask{}).Where("stream_key = ? AND kind = ? AND attempts > 0", t.StreamKey, "recovery").Count(&recoveries).Error; err != nil {
				return err
			}
			if recoveries > 0 {
				return setState(Manual, "recovery was already attempted; an unsent stale problem must not reopen this incident")
			}
		} else {
			// Recovery duplication protection is independent of fault reminder counts.
			var previousRecoveries int64
			if err := tx.Model(&models.ExternalPushTask{}).Where("stream_key = ? AND kind = ? AND id <> ? AND attempts > 0", t.StreamKey, "recovery", t.ID).Count(&previousRecoveries).Error; err != nil {
				return err
			}
			if previousRecoveries > 0 {
				return suppress("suppress_recovery", "duplicate recovery suppressed: this incident already has a recovery request attempt")
			}
			if anchor == nil {
				return setState(Waiting, "no problem has been delivered for this destination and incident; no synthetic problem created")
			}
			if t.Attempts == 0 || s.Config.EffectiveSigningMode() != "key_file" {
				current, _ := Build(s)
				var original Payload
				if err := json.Unmarshal([]byte(anchor.Payload), &original); err != nil {
					return setState(Invalid, "invalid predecessor payload")
				}
				current.CompanyID = original.CompanyID
				current.DeviceType = original.DeviceType
				current.DeviceName = original.DeviceName
				current.ProblemTime = original.ProblemTime
				current.TriggerName = original.TriggerName
				current.Level = original.Level
				current.DeviceTags = original.DeviceTags
				encoded, err := json.Marshal(current)
				if err != nil {
					return err
				}
				t.Payload = string(encoded)
			}
		}
		var previous Snapshot
		if anchor != nil {
			if json.Unmarshal([]byte(anchor.Snapshot), &previous) != nil {
				return setState(Invalid, "invalid predecessor payload")
			}
			if previous.Config.Mode != s.Config.Mode {
				return setState(Invalid, "receiver mode differs from accepted problem; simulation and real incidents cannot be paired")
			}
			if previous.Config.EffectiveSigningMode() != s.Config.EffectiveSigningMode() {
				return setState(Invalid, "signing inputs differ from accepted problem; restore the original key version")
			}
		}
		if s.Config.EffectiveSigningMode() == "key_file" {
			var final Payload
			if err := json.Unmarshal([]byte(t.Payload), &final); err != nil {
				return setState(Invalid, "invalid persisted protocol payload")
			}
			signature, err := w.Keys.Sign(final.ProblemTime, s.Config.KeyReference())
			if err != nil {
				state := Invalid
				if t.Attempts > 0 {
					state = Manual
				}
				return setState(state, err.Error())
			}
			if t.Attempts > 0 && final.Sign != signature {
				return setState(Manual, "signing key no longer matches frozen request; restore key version before explicit resend")
			}
			if anchor != nil {
				var original Payload
				if json.Unmarshal([]byte(anchor.Payload), &original) != nil {
					return setState(Invalid, "invalid predecessor payload")
				}
				if previous.Config.KeyReference() != s.Config.KeyReference() || original.ProblemTime != final.ProblemTime || original.Sign != signature {
					return setState(Invalid, "signing inputs differ from accepted problem; restore the original key version")
				}
			}
			if t.Attempts == 0 {
				final.Sign = signature
				encoded, err := json.Marshal(final)
				if err != nil {
					return err
				}
				t.Payload = string(encoded)
			}
		}
		a := models.ExternalPushAttempt{TaskID: id, Number: t.Attempts + 1, Token: token, State: Sending, StartedAt: now}
		r = tx.Model(&models.ExternalPushTask{}).Where("id = ? AND version = ? AND state IN ?", id, t.Version, []string{Pending, Waiting}).Updates(map[string]interface{}{"state": Sending, "reason": "request attempt recorded before network I/O", "version": t.Version + 1, "updated_at": now, "payload": t.Payload, "attempts": a.Number, "lease_token": token, "lease_until": now + leaseMS})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return release()
		}
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		claimed = &t
		attempt = &a
		return nil
	})
	return claimed, attempt, err
}

func send(ctx context.Context, t models.ExternalPushTask) (string, string) {
	var s Snapshot
	if err := json.Unmarshal([]byte(t.Snapshot), &s); err != nil {
		return Invalid, "invalid persisted snapshot"
	}
	_, err := Build(s)
	if err != nil {
		return Invalid, err.Error()
	}
	// Recovery pairs against immutable first-problem fields rather than a later
	// renamed rule. Enqueue validates the inputs; the worker uses its snapshot.
	raw := []byte(t.Payload)
	if !json.Valid(raw) {
		return Invalid, "invalid persisted protocol payload"
	}
	if s.Config.EffectiveSigningMode() == "key_file" {
		var p Payload
		if json.Unmarshal(raw, &p) != nil || len(p.Sign) != 32 || strings.Trim(p.Sign, "0123456789ABCDEF") != "" {
			return Invalid, "file signature was not prepared before sending"
		}
	}
	form := url.Values{"json": {string(raw)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.Endpoint, strings.NewReader(form))
	if err != nil {
		return Invalid, "cannot construct external push request"
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: time.Duration(s.Config.TimeoutMS) * time.Millisecond, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return Unknown, "transport interrupted or timed out; receiver may have saved the alarm (no automatic retry)"
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	summary := fmt.Sprintf("HTTP %d; bytes=%d; response_sha256=%s", resp.StatusCode, len(body), digest(string(body)))
	if err != nil || len(body) > 65536 {
		return Unknown, summary + "; incomplete or oversized response"
	}
	status, err := responseStatus(body)
	if err != nil {
		return Unknown, summary + "; " + err.Error()
	}
	code := status.String()
	if len(code) > 32 {
		code = code[:32] + "..."
	}
	summary += "; business_status=" + code
	if !statusIs200(status) || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Unknown, summary + "; acceptance not confirmed"
	}
	return Accepted, summary + "; 1.0 interface reports successful reception only (downstream notification unverified)"
}
