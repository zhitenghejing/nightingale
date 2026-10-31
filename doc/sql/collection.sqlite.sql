-- SNMP collection management schema. Contains no credentials or runtime data.

CREATE TABLE IF NOT EXISTS `monitor_collector` (`id` text,`name` text,`agent_ident` text,`datasource_id` integer,`networks` TEXT,`ports` TEXT,`token_hash` text,`previous_token_hash` text,`previous_token_until` integer,`writer_user` text,`enabled` numeric,`record_version` integer,`desired_version` text,`last_served_version` text,`last_client_version` text,`last_fetched_at` integer,`client_seen_at` integer,PRIMARY KEY (`id`),CONSTRAINT `uni_monitor_collector_writer_user` UNIQUE (`writer_user`));

CREATE TABLE IF NOT EXISTS `monitor_collector_group` (`collector_id` text,`group_id` integer,PRIMARY KEY (`collector_id`,`group_id`));

CREATE TABLE IF NOT EXISTS `monitor_credential` (`id` text,`name` text,`group_id` integer,`kind` text,`ciphertext` TEXT,`key_id` text,`record_version` integer,`updated_at` integer,PRIMARY KEY (`id`));

CREATE INDEX `idx_monitor_credential_group_id` ON `monitor_credential`(`group_id`);

CREATE TABLE IF NOT EXISTS `monitor_device` (`id` text,`name` text,`ident` text,`address` text,`port` integer,`protocol` text,`group_id` integer,`collector_id` text,`credential_id` text,`profile_state` LONGTEXT,`parameters` LONGTEXT,`notify` LONGTEXT,`template_version_id` text,`interval_seconds` integer,`timeout_seconds` integer,`retries` integer,`labels` TEXT,`cmdb_id` text,`enabled` numeric,`deleted` numeric,`record_version` integer,`desired_revision` text,`publication_version` text,`withdraw_version` text,`withdraw_at` integer,`withdraw_interval_seconds` integer,`last_received_at` integer,`last_rejected_at` integer,`last_clock_error_at` integer,`created_at` integer,`updated_at` integer,`group_endpoint` text,`collector_endpoint` text,`request_key` text,`request_hash` text,PRIMARY KEY (`id`),CONSTRAINT `uni_monitor_device_ident` UNIQUE (`ident`),CONSTRAINT `uni_monitor_device_request_key` UNIQUE (`request_key`));

CREATE INDEX `idx_monitor_device_deleted` ON `monitor_device`(`deleted`);

CREATE INDEX `idx_monitor_device_credential_id` ON `monitor_device`(`credential_id`);

CREATE INDEX `idx_monitor_device_collector_id` ON `monitor_device`(`collector_id`);

CREATE INDEX `idx_monitor_device_group_id` ON `monitor_device`(`group_id`);

CREATE TABLE IF NOT EXISTS `monitor_template_version` (`id` text,`template_key` text,`version` integer,`name` text,`definition` TEXT,`digest` text,PRIMARY KEY (`id`));

CREATE TABLE IF NOT EXISTS `monitor_publication` (`id` text,`collector_id` text,`version` text,`ciphertext` TEXT,`key_id` text,`digest` text,`manifest` TEXT,`reason` text,`created_by` integer,`created_at` integer,PRIMARY KEY (`id`),CONSTRAINT `uni_monitor_publication_version` UNIQUE (`version`));

CREATE INDEX `idx_monitor_publication_collector_id` ON `monitor_publication`(`collector_id`);

CREATE TABLE IF NOT EXISTS `monitor_probe` (`id` text,`group_id` integer,`collector_id` text,`created_by` integer,`ciphertext` TEXT,`key_id` text,`revision` text,`deadline` integer,`created_at` integer,`cleanup_version` text,`cleanup_at` integer,`last_received_at` integer,`state` text,`result` text,PRIMARY KEY (`id`));

CREATE INDEX `idx_monitor_probe_created_by` ON `monitor_probe`(`created_by`);

CREATE INDEX `idx_monitor_probe_collector_id` ON `monitor_probe`(`collector_id`);

CREATE INDEX `idx_monitor_probe_group_id` ON `monitor_probe`(`group_id`);

CREATE TABLE IF NOT EXISTS `monitor_audit` (`id` text,`actor_id` integer,`group_id` integer,`object_id` text,`action` text,`detail` TEXT,`publication` text,`created_at` integer,PRIMARY KEY (`id`));

CREATE INDEX `idx_monitor_audit_object_id` ON `monitor_audit`(`object_id`);

CREATE INDEX `idx_monitor_audit_group_id` ON `monitor_audit`(`group_id`);

-- Template catalog: display metadata, categories and unpublished drafts.
CREATE TABLE IF NOT EXISTS "monitor_category" ("id" varchar(36),"parent_id" varchar(36),"name" varchar(128),"sort" bigint,"disabled" boolean,"record_version" bigint,PRIMARY KEY ("id"));
CREATE INDEX IF NOT EXISTS "idx_monitor_category_parent_id" ON "monitor_category" ("parent_id");
CREATE TABLE IF NOT EXISTS "monitor_catalog" ("component_id" bigint,"name" varchar(128),"logo" TEXT,"category_id" varchar(36),"disabled" boolean,"deleted" boolean,"draft" TEXT,"published_version_id" varchar(64),"record_version" bigint,PRIMARY KEY ("component_id"));
CREATE INDEX IF NOT EXISTS "idx_monitor_catalog_category_id" ON "monitor_catalog" ("category_id");
