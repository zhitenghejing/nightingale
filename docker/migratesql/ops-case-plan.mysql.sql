-- Existing pre-plan ops tables only. Run once. Normal Center startup uses AutoMigrate.
ALTER TABLE `ops_case` ADD COLUMN `plan_id` BIGINT NOT NULL DEFAULT 0;
ALTER TABLE `ops_step` ADD COLUMN `plan_id` BIGINT NOT NULL DEFAULT 0;
ALTER TABLE `ops_action` ADD COLUMN `plan_id` BIGINT NOT NULL DEFAULT 0;
ALTER TABLE `ops_verification` ADD COLUMN `plan_id` BIGINT NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS `ops_plan` (`id` bigint AUTO_INCREMENT,`case_id` bigint NOT NULL,`sequence` bigint NOT NULL,`title` varchar(255) NOT NULL,`scope` text,`rationale` text,`payload` LONGTEXT,`evidence_ids` LONGTEXT,`sha256` varchar(64) NOT NULL,`generation` bigint,`evidence_anchor` bigint,`confirmed_generation` bigint,`state` varchar(24) NOT NULL,`created_by` bigint,`created_at` bigint,`confirmed_by` bigint,`confirmed_at` bigint,`decision_note` text,PRIMARY KEY (`id`),UNIQUE INDEX `idx_ops_plan_case_sequence` (`case_id`,`sequence`));
