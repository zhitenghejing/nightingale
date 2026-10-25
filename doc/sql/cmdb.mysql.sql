-- CMDB inventory schema. Uses the configured center database; contains no asset data.

CREATE TABLE IF NOT EXISTS `cmdb_room` (
    `id` varchar(36) PRIMARY KEY,
    `name` varchar(160),
    UNIQUE KEY `idx_cmdb_room_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_rack` (
    `id` varchar(36) PRIMARY KEY,
    `room_id` varchar(36),
    `name` varchar(160),
    `units` bigint,
    `version` bigint,
    UNIQUE KEY `rack_name` (`room_id`, `name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_device` (
    `id` varchar(36) PRIMARY KEY,
    `serial` varchar(240),
    `rack_id` varchar(36),
    `start` bigint,
    `end` bigint,
    `version` bigint,
    `deleted` boolean,
    `payload` longtext,
    `updated_at` datetime(3),
    UNIQUE KEY `idx_cmdb_device_serial` (`serial`),
    KEY `idx_cmdb_device_rack_id` (`rack_id`),
    KEY `idx_cmdb_device_deleted` (`deleted`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_device_disk` (
    `id` bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    `asset_id` varchar(36),
    `position` bigint,
    `model` varchar(240),
    `description` text,
    `count` bigint,
    KEY `idx_cmdb_device_disk_asset_id` (`asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_change` (
    `id` bigint unsigned AUTO_INCREMENT PRIMARY KEY,
    `asset_id` varchar(36),
    `user_id` bigint,
    `action` varchar(40),
    `before` longtext,
    `after` longtext,
    `created_at` datetime(3),
    KEY `idx_cmdb_change_asset_id` (`asset_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_access` (
    `user_id` bigint PRIMARY KEY,
    `read` boolean,
    `create` boolean,
    `edit` boolean,
    `delete` boolean,
    `import` boolean,
    `export` boolean
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `cmdb_write_lock` (
    `id` bigint PRIMARY KEY,
    `revision` bigint
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

INSERT IGNORE INTO `cmdb_write_lock` (`id`, `revision`) VALUES (1, 0);
