-- Existing pre-plan ops tables only. Run once. Normal Center startup uses AutoMigrate.
ALTER TABLE "ops_case" ADD COLUMN "plan_id" INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "ops_step" ADD COLUMN "plan_id" INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "ops_action" ADD COLUMN "plan_id" INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "ops_verification" ADD COLUMN "plan_id" INTEGER NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS `ops_plan` (`id` integer PRIMARY KEY AUTOINCREMENT,`case_id` integer NOT NULL,`sequence` integer NOT NULL,`title` varchar(255) NOT NULL,`scope` text,`rationale` text,`payload` TEXT,`evidence_ids` TEXT,`sha256` varchar(64) NOT NULL,`generation` integer,`evidence_anchor` integer,`confirmed_generation` integer,`state` varchar(24) NOT NULL,`created_by` integer,`created_at` integer,`confirmed_by` integer,`confirmed_at` integer,`decision_note` text);
CREATE UNIQUE INDEX IF NOT EXISTS `idx_ops_plan_case_sequence` ON `ops_plan`(`case_id`,`sequence`);
