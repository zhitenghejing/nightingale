package models

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExternalPushType retains its persisted identifier as the display name evolves.
const ExternalPushType = "external_push"
const ExternalPushPlatform10Endpoint = "https://admin.ztservice.cn/messageWarn!acceptWarn.act"

// Event types, UTF-8 signing, and POST form transport are owner-confirmed.
// These constants do not establish the receiver's downstream notification result.
const ExternalPushProblemType = "problem"
const ExternalPushRecoveryType = "ok"
const ExternalPushDefaultKeyRef = "platform10-v1"

var externalPushKeyReference = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type ExternalPushConfig struct {
	SigningMode   string `json:"signing_mode,omitempty"`
	SigningKeyRef string `json:"signing_key_ref,omitempty"`
	Mode          string `json:"mode"`
	Endpoint      string `json:"endpoint"`
	Destination   string `json:"destination"`
	TimeoutMS     int    `json:"timeout_ms"`
	ProblemType   string `json:"problem_type"`
	RecoveryType  string `json:"recovery_type"`
	TestSign      string `json:"test_sign"`
	TimeZone      string `json:"time_zone"`
}

type ExternalPushTask struct {
	ID           int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	DedupeKey    string `json:"-" gorm:"type:varchar(64);not null;uniqueIndex:ux_ep_task_dedupe"`
	StreamKey    string `json:"-" gorm:"type:varchar(64);not null;index:idx_ep_task_stream"`
	Destination  string `json:"destination" gorm:"type:varchar(128);not null"`
	DispatchID   string `json:"dispatch_id" gorm:"type:varchar(36);not null"`
	IncidentID   string `json:"incident_id" gorm:"type:varchar(36);not null"`
	Sequence     int64  `json:"sequence" gorm:"not null"`
	Kind         string `json:"kind" gorm:"type:varchar(16);not null"`
	EventID      int64  `json:"event_id"`
	NotifyRuleID int64  `json:"notify_rule_id"`
	ChannelID    int64  `json:"channel_id"`
	State        string `json:"state" gorm:"type:varchar(32);not null;index:idx_ep_task_state"`
	Reason       string `json:"reason" gorm:"type:varchar(1024);not null"`
	Snapshot     string `json:"-" gorm:"type:text;not null"`
	Fingerprint  string `json:"-" gorm:"type:varchar(64);not null"`
	Payload      string `json:"-" gorm:"type:text;not null"`
	Version      int64  `json:"version" gorm:"not null"`
	Attempts     int    `json:"attempts" gorm:"not null"`
	LeaseToken   string `json:"-" gorm:"type:varchar(36);not null"`
	LeaseUntil   int64  `json:"lease_until" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"not null"`
}

func (ExternalPushTask) TableName() string { return "external_push_task" }

type ExternalPushStream struct {
	Key        string `json:"-" gorm:"column:stream_key;type:varchar(64);primaryKey"`
	LeaseToken string `json:"-" gorm:"type:varchar(36);not null"`
	LeaseUntil int64  `json:"-" gorm:"not null"`
}

func (ExternalPushStream) TableName() string { return "external_push_stream" }

type ExternalPushAttempt struct {
	ID         int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID     int64  `json:"task_id" gorm:"not null;index:idx_ep_attempt_task"`
	Number     int    `json:"number" gorm:"not null"`
	Token      string `json:"-" gorm:"type:varchar(36);not null"`
	State      string `json:"state" gorm:"type:varchar(32);not null"`
	Summary    string `json:"summary" gorm:"type:varchar(1024);not null"`
	StartedAt  int64  `json:"started_at" gorm:"not null"`
	FinishedAt int64  `json:"finished_at" gorm:"not null"`
}

func (ExternalPushAttempt) TableName() string { return "external_push_attempt" }

type ExternalPushOperation struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID      int64  `json:"task_id" gorm:"not null;index:idx_ep_operation_task"`
	Actor       string `json:"actor" gorm:"type:varchar(128);not null"`
	Action      string `json:"action" gorm:"type:varchar(64);not null"`
	Reason      string `json:"reason" gorm:"type:varchar(1024);not null"`
	BeforeState string `json:"before_state" gorm:"type:varchar(32);not null"`
	AfterState  string `json:"after_state" gorm:"type:varchar(32);not null"`
	CreatedAt   int64  `json:"created_at" gorm:"not null"`
}

func (ExternalPushOperation) TableName() string { return "external_push_operation" }

func (c *ExternalPushConfig) Verify() error {
	if c == nil {
		return errors.New("external push simulation configuration is required")
	}
	switch c.Mode {
	case "simulation":
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Port() == "" {
			return errors.New("simulation endpoint must be http://numeric-loopback:port/path without query, credentials or fragment")
		}
		ip := net.ParseIP(u.Hostname())
		port, err := strconv.Atoi(u.Port())
		if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
			return errors.New("simulation endpoint must use a numeric loopback IP and a valid port; production destinations are blocked")
		}
	case "platform10":
		if c.Endpoint != ExternalPushPlatform10Endpoint {
			return errors.New("platform 1.0 mode requires the confirmed HTTPS endpoint without query parameters")
		}
		if c.EffectiveSigningMode() != "key_file" {
			return errors.New("platform 1.0 mode requires a center key file; test signatures cannot be sent")
		}
	default:
		return errors.New("external push mode must be platform10 or simulation")
	}
	if strings.TrimSpace(c.Destination) == "" || len(c.Destination) > 128 {
		return errors.New("logical destination key is required (at most 128 bytes)")
	}
	if c.TimeoutMS < 1 || c.TimeoutMS > 10000 {
		return errors.New("simulation timeout_ms must be between 1 and 10000")
	}
	if c.ProblemType != ExternalPushProblemType || c.RecoveryType != ExternalPushRecoveryType {
		return errors.New("confirmed event types must be problem and ok; legacy simulation types require explicit configuration review")
	}
	switch c.EffectiveSigningMode() {
	case "test_value":
		if strings.TrimSpace(c.TestSign) == "" {
			return errors.New("an explicit test-only signature is required; production signing is unconfirmed")
		}
		if c.SigningKeyRef != "" {
			return errors.New("test signing must not select a key file reference")
		}
	case "key_file":
		if !externalPushKeyReference.MatchString(c.KeyReference()) {
			return errors.New("signing key reference must be a versioned identifier, not a file path or password")
		}
		if c.TestSign != "" {
			return errors.New("file signing must not contain a test signature or private key")
		}
	default:
		return errors.New("signing mode must be test_value or key_file")
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil || c.TimeZone == "" {
		return errors.New("explicit valid time_zone is required")
	}
	return nil
}

// Empty mode preserves existing explicit simulation configurations without
// rewriting their durable snapshots or enabling file signing implicitly.
func (c ExternalPushConfig) EffectiveSigningMode() string {
	if c.SigningMode == "" {
		return "test_value"
	}
	return c.SigningMode
}
func (c ExternalPushConfig) KeyReference() string {
	if c.SigningKeyRef == "" {
		return ExternalPushDefaultKeyRef
	}
	return c.SigningKeyRef
}
