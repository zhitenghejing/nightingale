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
  updated_at bigint,
  INDEX idx_ops_credential_group_id (group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

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
  updated_at bigint,
  INDEX idx_ops_ssh_connection_group_id (group_id),
  INDEX idx_ops_ssh_connection_credential_id (credential_id),
  INDEX idx_ops_ssh_connection_jump_credential_id (jump_credential_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS ops_access_audit (
  id varchar(36) PRIMARY KEY,
  actor_id bigint,
  group_id bigint,
  object_id varchar(191),
  action varchar(48),
  created_at bigint,
  INDEX idx_ops_access_audit_group_id (group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
