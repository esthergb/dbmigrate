-- Phase 62 MySQL hidden-schema fixture
-- Replace __DB__ with the target schema name before execution.

DROP DATABASE IF EXISTS `__DB__`;
CREATE DATABASE `__DB__` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `__DB__`;

CREATE TABLE `invisible_demo` (
  `id` INT NOT NULL,
  `visible_name` VARCHAR(32) NOT NULL,
  `secret_token` VARCHAR(32) INVISIBLE,
  PRIMARY KEY (`id`),
  KEY `idx_secret_token` (`secret_token`) INVISIBLE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO `invisible_demo` (`id`, `visible_name`, `secret_token`)
VALUES
  (1, 'visible', 'secret'),
  (2, 'visible-two', 'secret-two');

SET SESSION sql_generate_invisible_primary_key = ON;

CREATE TABLE `gipk_demo` (
  `visible_name` VARCHAR(32) NOT NULL,
  `created_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO `gipk_demo` (`visible_name`)
VALUES
  ('row-one'),
  ('row-two');
