DROP DATABASE IF EXISTS `__DB__`;
CREATE DATABASE `__DB__` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE `__DB__`;

CREATE TABLE `message_0900` (
  `id` INT NOT NULL,
  `title` VARCHAR(128) COLLATE utf8mb4_0900_ai_ci NOT NULL,
  `body` TEXT COLLATE utf8mb4_0900_ai_ci NOT NULL,
  `tag` VARCHAR(32) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `message_0900` (`id`, `title`, `body`, `tag`) VALUES
  (1, 'alpha', 'phase63 mysql 0900 fixture', 'plan'),
  (2, 'beta', 'server unsupported collation rehearsal', 'report');
