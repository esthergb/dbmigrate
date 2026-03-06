DROP DATABASE IF EXISTS `__DB__`;
CREATE DATABASE `__DB__` CHARACTER SET utf8mb4 COLLATE utf8mb4_uca1400_ai_ci;
USE `__DB__`;

CREATE TABLE `message_uca1400` (
  `id` INT NOT NULL,
  `title` VARCHAR(128) COLLATE utf8mb4_uca1400_ai_ci NOT NULL,
  `body` TEXT COLLATE utf8mb4_uca1400_ai_ci NOT NULL,
  `tag` VARCHAR(32) NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_uca1400_ai_ci;

INSERT INTO `message_uca1400` (`id`, `title`, `body`, `tag`) VALUES
  (1, 'gamma', 'phase63 mariadb uca1400 fixture', 'plan'),
  (2, 'delta', 'client compatibility risk rehearsal', 'report');
