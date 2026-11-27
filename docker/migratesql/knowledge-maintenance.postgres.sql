-- One-time upgrade for existing archive knowledge installations.
ALTER TABLE "knowledge_document_version" ADD COLUMN "summary_model" varchar(255) DEFAULT '';
ALTER TABLE "knowledge_document_version" ADD COLUMN "summary_recipe" varchar(64) DEFAULT '';
ALTER TABLE "knowledge_document_version" ADD COLUMN "parser_revision" varchar(32) DEFAULT '';
ALTER TABLE "knowledge_document_version" ADD COLUMN "index_revision" varchar(32) DEFAULT '';
CREATE TABLE IF NOT EXISTS "knowledge_document_term" ("term" varchar(64),"section_id" bigint,"version_id" bigint NOT NULL,"weight" bigint NOT NULL,PRIMARY KEY ("term","section_id"));
CREATE INDEX IF NOT EXISTS "idx_knowledge_document_term_version_id" ON "knowledge_document_term" ("version_id");
CREATE TABLE IF NOT EXISTS "knowledge_document_derivation" ("section_id" bigint,"version_id" bigint NOT NULL,"source_digest" varchar(64),"recipe" varchar(64),"model" varchar(255),"state" varchar(32),"content" TEXT,"attempts" bigint,"next_run" bigint,"error" varchar(500),PRIMARY KEY ("section_id"));
CREATE INDEX IF NOT EXISTS "idx_knowledge_document_derivation_version_id" ON "knowledge_document_derivation" ("version_id");
