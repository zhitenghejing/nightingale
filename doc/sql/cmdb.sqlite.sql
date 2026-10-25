-- CMDB inventory schema. Uses the configured center database; contains no asset data.

CREATE TABLE IF NOT EXISTS "cmdb_room" (
    "id" varchar(36) PRIMARY KEY,
    "name" varchar(160)
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_cmdb_room_name" ON "cmdb_room" ("name");

CREATE TABLE IF NOT EXISTS "cmdb_rack" (
    "id" varchar(36) PRIMARY KEY,
    "room_id" varchar(36),
    "name" varchar(160),
    "units" bigint,
    "version" bigint
);

CREATE UNIQUE INDEX IF NOT EXISTS "rack_name" ON "cmdb_rack" ("room_id", "name");

CREATE TABLE IF NOT EXISTS "cmdb_device" (
    "id" varchar(36) PRIMARY KEY,
    "serial" varchar(240),
    "rack_id" varchar(36),
    "start" bigint,
    "end" bigint,
    "version" bigint,
    "deleted" boolean,
    "payload" text,
    "updated_at" datetime
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_cmdb_device_serial" ON "cmdb_device" ("serial");

CREATE INDEX IF NOT EXISTS "idx_cmdb_device_rack_id" ON "cmdb_device" ("rack_id");

CREATE INDEX IF NOT EXISTS "idx_cmdb_device_deleted" ON "cmdb_device" ("deleted");

CREATE TABLE IF NOT EXISTS "cmdb_device_disk" (
    "id" integer PRIMARY KEY AUTOINCREMENT,
    "asset_id" varchar(36),
    "position" bigint,
    "model" varchar(240),
    "description" text,
    "count" bigint
);

CREATE INDEX IF NOT EXISTS "idx_cmdb_device_disk_asset_id" ON "cmdb_device_disk" ("asset_id");

CREATE TABLE IF NOT EXISTS "cmdb_change" (
    "id" integer PRIMARY KEY AUTOINCREMENT,
    "asset_id" varchar(36),
    "user_id" bigint,
    "action" varchar(40),
    "before" text,
    "after" text,
    "created_at" datetime
);

CREATE INDEX IF NOT EXISTS "idx_cmdb_change_asset_id" ON "cmdb_change" ("asset_id");

CREATE TABLE IF NOT EXISTS "cmdb_access" (
    "user_id" bigint PRIMARY KEY,
    "read" boolean,
    "create" boolean,
    "edit" boolean,
    "delete" boolean,
    "import" boolean,
    "export" boolean
);

CREATE TABLE IF NOT EXISTS "cmdb_write_lock" (
    "id" bigint PRIMARY KEY,
    "revision" bigint
);

INSERT INTO "cmdb_write_lock" ("id", "revision") VALUES (1, 0) ON CONFLICT DO NOTHING;
