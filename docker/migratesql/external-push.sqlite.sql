
-- External push lifecycle identity (run once on existing databases).
ALTER TABLE alert_cur_event ADD COLUMN dispatch_id varchar(36) NOT NULL DEFAULT '';
ALTER TABLE alert_cur_event ADD COLUMN incident_id varchar(36) NOT NULL DEFAULT '';
ALTER TABLE alert_cur_event ADD COLUMN incident_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE alert_his_event ADD COLUMN dispatch_id varchar(36) NOT NULL DEFAULT '';
ALTER TABLE alert_his_event ADD COLUMN incident_id varchar(36) NOT NULL DEFAULT '';
ALTER TABLE alert_his_event ADD COLUMN incident_sequence bigint NOT NULL DEFAULT 0;

-- Dedicated external push simulation ledger.
CREATE TABLE IF NOT EXISTS external_push_task (
    id integer PRIMARY KEY AUTOINCREMENT,
    dedupe_key varchar(64) NOT NULL,
    stream_key varchar(64) NOT NULL,
    destination varchar(128) NOT NULL,
    dispatch_id varchar(36) NOT NULL,
    incident_id varchar(36) NOT NULL,
    sequence bigint NOT NULL,
    kind varchar(16) NOT NULL,
    event_id bigint,
    notify_rule_id bigint,
    channel_id bigint,
    state varchar(32) NOT NULL,
    reason varchar(1024) NOT NULL,
    snapshot text NOT NULL,
    fingerprint varchar(64) NOT NULL,
    payload text NOT NULL,
    version bigint NOT NULL,
    attempts bigint NOT NULL,
    lease_token varchar(36) NOT NULL,
    lease_until bigint NOT NULL,
    created_at bigint NOT NULL,
    updated_at bigint NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_ep_task_dedupe ON external_push_task (dedupe_key);
CREATE INDEX IF NOT EXISTS idx_ep_task_stream ON external_push_task (stream_key);
CREATE INDEX IF NOT EXISTS idx_ep_task_state ON external_push_task (state);
CREATE TABLE IF NOT EXISTS external_push_stream (
    stream_key varchar(64) PRIMARY KEY,
    lease_token varchar(36) NOT NULL,
    lease_until bigint NOT NULL
);
CREATE TABLE IF NOT EXISTS external_push_attempt (
    id integer PRIMARY KEY AUTOINCREMENT,
    task_id bigint NOT NULL,
    number bigint NOT NULL,
    token varchar(36) NOT NULL,
    state varchar(32) NOT NULL,
    summary varchar(1024) NOT NULL,
    started_at bigint NOT NULL,
    finished_at bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ep_attempt_task ON external_push_attempt (task_id);
CREATE TABLE IF NOT EXISTS external_push_operation (
    id integer PRIMARY KEY AUTOINCREMENT,
    task_id bigint NOT NULL,
    actor varchar(128) NOT NULL,
    action varchar(64) NOT NULL,
    reason varchar(1024) NOT NULL,
    before_state varchar(32) NOT NULL,
    after_state varchar(32) NOT NULL,
    created_at bigint NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ep_operation_task ON external_push_operation (task_id);
