DROP DATABASE IF EXISTS `__DB__`;
CREATE DATABASE `__DB__` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `__DB__`;

SET SESSION time_zone = '+00:00';

CREATE TABLE `collation_order_demo` (
  `id` INT NOT NULL,
  `label` VARCHAR(32) COLLATE utf8mb4_unicode_ci NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO `collation_order_demo` (`id`, `label`) VALUES
  (5, 'Å'),
  (3, 's'),
  (2, 'ß'),
  (1, 'ss'),
  (4, 'z');

CREATE TABLE `temporal_demo` (
  `id` INT NOT NULL,
  `ts_value` TIMESTAMP NOT NULL,
  `dt_value` DATETIME NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `temporal_demo` (`id`, `ts_value`, `dt_value`) VALUES
  (2, '2024-06-07 08:09:10', '2024-06-07 08:09:10'),
  (1, '2024-01-02 03:04:05', '2024-01-02 03:04:05');

CREATE TABLE `json_demo` (
  `id` INT NOT NULL,
  `payload` JSON NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `json_demo` (`id`, `payload`) VALUES
  (1, '{"b":2,"a":1}'),
  (2, '{"a":4,"b":3}');

CREATE TABLE `numeric_demo` (
  `id` INT NOT NULL,
  `approx_value` DOUBLE NOT NULL,
  `exact_value` DECIMAL(10,4) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `numeric_demo` (`id`, `approx_value`, `exact_value`) VALUES
  (2, 1.0 / 3.0, 0.3333),
  (1, 0.3, 0.3000);
