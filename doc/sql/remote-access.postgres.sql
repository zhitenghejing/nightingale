-- Encrypted SSH operations credentials and device bindings. No key material.

CREATE TABLE IF NOT EXISTS ops_credential (
  id varchar(36) PRIMARY KEY,
  name varchar(128) NOT NULL,
  group_id bigint NOT NULL,
  disabled boolean NOT NULL DEFAULT false,
  device_only BOOLEAN NOT NULL DEFAULT FALSE,
  ciphertext text NOT NULL,
  key_id varchar(64) NOT NULL,
  record_version bigint NOT NULL,
  updated_by bigint,
  updated_at bigint
);

CREATE INDEX IF NOT EXISTS idx_ops_credential_group_id ON ops_credential (group_id);

CREATE TABLE IF NOT EXISTS ops_ssh_connection (
  target_ident varchar(191) PRIMARY KEY,
  target_id bigint NOT NULL,
  group_id bigint NOT NULL,
  enabled boolean NOT NULL,
  protocol VARCHAR(8) NOT NULL DEFAULT 'ssh',
  host varchar(64) NOT NULL,
  port bigint NOT NULL,
  fingerprint varchar(128) NOT NULL,
  credential_id varchar(36) NOT NULL,
  jump_host varchar(64),
  jump_port bigint,
  jump_fingerprint varchar(128),
  jump_credential_id varchar(36),
  log_paths_json text,
  record_version bigint NOT NULL,
  updated_by bigint,
  updated_at bigint
);

CREATE INDEX IF NOT EXISTS idx_ops_ssh_connection_group_id ON ops_ssh_connection (group_id);

CREATE INDEX IF NOT EXISTS idx_ops_ssh_connection_credential_id ON ops_ssh_connection (credential_id);

CREATE INDEX IF NOT EXISTS idx_ops_ssh_connection_jump_credential_id ON ops_ssh_connection (jump_credential_id);

CREATE TABLE IF NOT EXISTS ops_access_audit (
  id varchar(36) PRIMARY KEY,
  actor_id bigint,
  group_id bigint,
  object_id varchar(191),
  action varchar(48),
  created_at bigint
);

CREATE INDEX IF NOT EXISTS idx_ops_access_audit_group_id ON ops_access_audit (group_id);
