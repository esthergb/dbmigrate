DROP DATABASE IF EXISTS `__DB__`;
CREATE DATABASE `__DB__` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE `__DB__`;

SET SESSION time_zone = '+00:00';

CREATE TABLE `collation_order_demo` (
  `id` INT NOT NULL,
  `label` VARCHAR(32) COLLATE utf8mb4_0900_ai_ci NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `collation_order_demo` (`id`, `label`) VALUES
  (1, 'ss'),
  (2, 'ß'),
  (3, 's'),
  (4, 'z'),
  (5, 'Å');

CREATE TABLE `temporal_demo` (
  `id` INT NOT NULL,
  `ts_value` TIMESTAMP NOT NULL,
  `dt_value` DATETIME NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `temporal_demo` (`id`, `ts_value`, `dt_value`) VALUES
  (1, '2024-01-02 03:04:05', '2024-01-02 03:04:05'),
  (2, '2024-06-07 08:09:10', '2024-06-07 08:09:10');

CREATE TABLE `json_demo` (
  `id` INT NOT NULL,
  `payload` JSON NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `json_demo` (`id`, `payload`) VALUES
  (1, JSON_OBJECT('a', 1, 'b', 2)),
  (2, JSON_OBJECT('b', 3, 'a', 4));

CREATE TABLE `numeric_demo` (
  `id` INT NOT NULL,
  `approx_value` DOUBLE NOT NULL,
  `exact_value` DECIMAL(10,4) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB;

INSERT INTO `numeric_demo` (`id`, `approx_value`, `exact_value`) VALUES
  (1, 0.1 + 0.2, 0.3000),
  (2, 1.0 / 3.0, 0.3333);
