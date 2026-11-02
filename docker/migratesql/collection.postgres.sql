-- SNMP collection management schema. Contains no credentials or runtime data.

CREATE TABLE IF NOT EXISTS "monitor_collector" ("id" varchar(36),"name" varchar(128),"agent_ident" varchar(191),"datasource_id" bigint,"networks" TEXT,"ports" TEXT,"token_hash" varchar(64),"previous_token_hash" varchar(64),"previous_token_until" bigint,"writer_user" varchar(191),"enabled" boolean,"record_version" bigint,"desired_version" varchar(64),"last_served_version" varchar(64),"last_client_version" varchar(64),"last_fetched_at" bigint,"client_seen_at" bigint,PRIMARY KEY ("id"),CONSTRAINT "uni_monitor_collector_writer_user" UNIQUE ("writer_user"));

CREATE TABLE IF NOT EXISTS "monitor_collector_group" ("collector_id" varchar(36),"group_id" bigint,PRIMARY KEY ("collector_id","group_id"));

CREATE TABLE IF NOT EXISTS "monitor_credential" ("id" varchar(36),"name" varchar(128),"group_id" bigint,"kind" varchar(32),"ciphertext" TEXT,"key_id" varchar(64),"record_version" bigint,"updated_at" bigint,PRIMARY KEY ("id"));

CREATE INDEX IF NOT EXISTS "idx_monitor_credential_group_id" ON "monitor_credential" ("group_id");

CREATE TABLE IF NOT EXISTS "monitor_device" ("id" varchar(36),"name" varchar(128),"ident" varchar(191),"address" varchar(45),"port" bigint,"protocol" varchar(32),"group_id" bigint,"collector_id" varchar(36),"credential_id" varchar(36),"profile_state" TEXT,"parameters" TEXT,"notify" TEXT,"template_version_id" varchar(64),"interval_seconds" bigint,"timeout_seconds" bigint,"retries" bigint,"labels" TEXT,"cmdb_id" varchar(36),"enabled" boolean,"deleted" boolean,"record_version" bigint,"desired_revision" varchar(64),"publication_version" varchar(64),"withdraw_version" varchar(64),"withdraw_at" bigint,"withdraw_interval_seconds" bigint,"last_received_at" bigint,"last_rejected_at" bigint,"last_clock_error_at" bigint,"created_at" bigint,"updated_at" bigint,"group_endpoint" varchar(64),"collector_endpoint" varchar(64),"request_key" varchar(64),"request_hash" varchar(64),PRIMARY KEY ("id"),CONSTRAINT "uni_monitor_device_ident" UNIQUE ("ident"),CONSTRAINT "uni_monitor_device_request_key" UNIQUE ("request_key"));

CREATE INDEX IF NOT EXISTS "idx_monitor_device_deleted" ON "monitor_device" ("deleted");

CREATE INDEX IF NOT EXISTS "idx_monitor_device_credential_id" ON "monitor_device" ("credential_id");

CREATE INDEX IF NOT EXISTS "idx_monitor_device_collector_id" ON "monitor_device" ("collector_id");

CREATE INDEX IF NOT EXISTS "idx_monitor_device_group_id" ON "monitor_device" ("group_id");

CREATE TABLE IF NOT EXISTS "monitor_template_version" ("id" varchar(64),"template_key" varchar(64),"version" bigint,"name" varchar(128),"definition" TEXT,"digest" varchar(64),PRIMARY KEY ("id"));

CREATE TABLE IF NOT EXISTS "monitor_publication" ("id" varchar(36),"collector_id" varchar(36),"version" varchar(64),"ciphertext" TEXT,"key_id" varchar(64),"digest" varchar(64),"manifest" TEXT,"reason" varchar(40),"created_by" bigint,"created_at" bigint,PRIMARY KEY ("id"),CONSTRAINT "uni_monitor_publication_version" UNIQUE ("version"));

CREATE INDEX IF NOT EXISTS "idx_monitor_publication_collector_id" ON "monitor_publication" ("collector_id");

CREATE TABLE IF NOT EXISTS "monitor_probe" ("id" varchar(36),"group_id" bigint,"collector_id" varchar(36),"created_by" bigint,"ciphertext" TEXT,"key_id" varchar(64),"revision" varchar(64),"deadline" bigint,"created_at" bigint,"cleanup_version" varchar(64),"cleanup_at" bigint,"last_received_at" bigint,"state" varchar(32),"result" varchar(32),PRIMARY KEY ("id"));

CREATE INDEX IF NOT EXISTS "idx_monitor_probe_created_by" ON "monitor_probe" ("created_by");

CREATE INDEX IF NOT EXISTS "idx_monitor_probe_collector_id" ON "monitor_probe" ("collector_id");

CREATE INDEX IF NOT EXISTS "idx_monitor_probe_group_id" ON "monitor_probe" ("group_id");

CREATE TABLE IF NOT EXISTS "monitor_audit" ("id" varchar(36),"actor_id" bigint,"group_id" bigint,"object_id" varchar(36),"action" varchar(40),"detail" TEXT,"publication" varchar(64),"created_at" bigint,PRIMARY KEY ("id"));

CREATE INDEX IF NOT EXISTS "idx_monitor_audit_object_id" ON "monitor_audit" ("object_id");

CREATE INDEX IF NOT EXISTS "idx_monitor_audit_group_id" ON "monitor_audit" ("group_id");

-- Template catalog: display metadata, categories and unpublished drafts.
CREATE TABLE IF NOT EXISTS "monitor_category" ("id" varchar(36),"parent_id" varchar(36),"name" varchar(128),"sort" bigint,"disabled" boolean,"record_version" bigint,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_monitor_category_parent_id" ON "monitor_category" ("parent_id");
CREATE TABLE IF NOT EXISTS "monitor_catalog" ("component_id" bigint,"name" varchar(128),"logo" TEXT,"category_id" varchar(36),"disabled" boolean,"deleted" boolean,"draft" TEXT,"published_version_id" varchar(64),"record_version" bigint,PRIMARY KEY ("component_id"));
CREATE INDEX IF NOT EXISTS "idx_monitor_catalog_category_id" ON "monitor_catalog" ("category_id");

-- Reusable template icon materials.
CREATE TABLE IF NOT EXISTS "monitor_icon" ("id" varchar(64),"name" varchar(128),"mime" varchar(32),"data" TEXT,"size" bigint,"created_at" bigint,PRIMARY KEY ("id"));
