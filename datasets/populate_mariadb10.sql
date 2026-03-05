-- MariaDB 10.x Compatible SQL Dataset
-- Stress testing dataset with users, databases, tables, and sample data
-- Compatible with MariaDB 10.0 through 10.6

SET GLOBAL max_connections = 1000;
SET SESSION sql_mode = 'STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION';

-- ============================================================================
-- CREATE USERS AND GRANT PRIVILEGES
-- ============================================================================

CREATE USER 'app_user'@'localhost' IDENTIFIED BY 'SecurePass123!';
CREATE USER 'analytics_user'@'localhost' IDENTIFIED BY 'AnalyticsPass456!';
CREATE USER 'reporting_user'@'localhost' IDENTIFIED BY 'ReportPass789!';
CREATE USER 'api_service'@'localhost' IDENTIFIED BY 'ApiServicePass012!';
CREATE USER 'batch_processor'@'localhost' IDENTIFIED BY 'BatchPass345!';
CREATE USER 'readonly_user'@'localhost' IDENTIFIED BY 'ReadOnlyPass678!';
CREATE USER 'admin_user'@'localhost' IDENTIFIED BY 'AdminPass901!';
CREATE USER 'backup_user'@'localhost' IDENTIFIED BY 'BackupPass234!';
CREATE USER 'migration_user'@'localhost' IDENTIFIED BY 'MigrationPass567!';
CREATE USER 'monitor_user'@'localhost' IDENTIFIED BY 'MonitorPass890!';

-- ============================================================================
-- CREATE DATABASES
-- ============================================================================

CREATE DATABASE ecommerce_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE analytics_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE crm_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE inventory_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE logs_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- ============================================================================
-- GRANT PRIVILEGES TO USERS
-- ============================================================================

GRANT ALL PRIVILEGES ON ecommerce_db.* TO 'app_user'@'localhost';
GRANT ALL PRIVILEGES ON ecommerce_db.* TO 'api_service'@'localhost';
GRANT SELECT, INSERT, UPDATE ON analytics_db.* TO 'analytics_user'@'localhost';
GRANT SELECT ON analytics_db.* TO 'reporting_user'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON inventory_db.* TO 'batch_processor'@'localhost';
GRANT SELECT ON ecommerce_db.* TO 'readonly_user'@'localhost';
GRANT SELECT ON analytics_db.* TO 'readonly_user'@'localhost';
GRANT SELECT ON inventory_db.* TO 'readonly_user'@'localhost';
GRANT ALL PRIVILEGES ON *.* TO 'admin_user'@'localhost' WITH GRANT OPTION;
GRANT LOCK TABLES, SELECT ON *.* TO 'backup_user'@'localhost';
GRANT ALL PRIVILEGES ON ecommerce_db.* TO 'migration_user'@'localhost';
GRANT ALL PRIVILEGES ON analytics_db.* TO 'migration_user'@'localhost';
GRANT SELECT, SHOW VIEW ON *.* TO 'monitor_user'@'localhost';

FLUSH PRIVILEGES;

-- ============================================================================
-- ECOMMERCE_DB SCHEMA
-- ============================================================================

USE ecommerce_db;

CREATE TABLE users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(100) NOT NULL UNIQUE,
  email VARCHAR(150) NOT NULL UNIQUE,
  password_hash VARCHAR(255) NOT NULL,
  first_name VARCHAR(100),
  last_name VARCHAR(100),
  phone VARCHAR(20),
  date_of_birth DATE,
  is_active BOOLEAN DEFAULT TRUE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_email (email),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE user_addresses (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  address_type ENUM('billing', 'shipping', 'home', 'work'),
  street_address VARCHAR(255) NOT NULL,
  city VARCHAR(100) NOT NULL,
  state_province VARCHAR(100),
  postal_code VARCHAR(20),
  country VARCHAR(100) NOT NULL,
  is_default BOOLEAN DEFAULT FALSE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  INDEX idx_user_id (user_id),
  INDEX idx_address_type (address_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE categories (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100) NOT NULL UNIQUE,
  description TEXT,
  parent_category_id INT,
  is_active BOOLEAN DEFAULT TRUE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (parent_category_id) REFERENCES categories(id) ON DELETE SET NULL,
  INDEX idx_parent_id (parent_category_id),
  INDEX idx_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE products (
  id INT AUTO_INCREMENT PRIMARY KEY,
  sku VARCHAR(50) NOT NULL UNIQUE,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  category_id INT NOT NULL,
  price DECIMAL(10, 2) NOT NULL,
  cost DECIMAL(10, 2),
  stock_quantity INT DEFAULT 0,
  is_active BOOLEAN DEFAULT TRUE,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE RESTRICT,
  INDEX idx_sku (sku),
  INDEX idx_category_id (category_id),
  INDEX idx_price (price),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE product_images (
  id INT AUTO_INCREMENT PRIMARY KEY,
  product_id INT NOT NULL,
  image_url VARCHAR(500) NOT NULL,
  alt_text VARCHAR(255),
  display_order INT DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
  INDEX idx_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE orders (
  id INT AUTO_INCREMENT PRIMARY KEY,
  order_number VARCHAR(50) NOT NULL UNIQUE,
  user_id INT NOT NULL,
  status ENUM('pending', 'confirmed', 'shipped', 'delivered', 'cancelled', 'returned') DEFAULT 'pending',
  total_amount DECIMAL(12, 2) NOT NULL,
  tax_amount DECIMAL(10, 2) DEFAULT 0,
  shipping_cost DECIMAL(10, 2) DEFAULT 0,
  discount_amount DECIMAL(10, 2) DEFAULT 0,
  shipping_address_id INT,
  billing_address_id INT,
  notes TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  shipped_at DATETIME,
  delivered_at DATETIME,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
  FOREIGN KEY (shipping_address_id) REFERENCES user_addresses(id) ON DELETE SET NULL,
  FOREIGN KEY (billing_address_id) REFERENCES user_addresses(id) ON DELETE SET NULL,
  INDEX idx_user_id (user_id),
  INDEX idx_status (status),
  INDEX idx_created_at (created_at),
  INDEX idx_order_number (order_number)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE order_items (
  id INT AUTO_INCREMENT PRIMARY KEY,
  order_id INT NOT NULL,
  product_id INT NOT NULL,
  quantity INT NOT NULL,
  unit_price DECIMAL(10, 2) NOT NULL,
  discount_percent DECIMAL(5, 2) DEFAULT 0,
  line_total DECIMAL(12, 2) NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT,
  INDEX idx_order_id (order_id),
  INDEX idx_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE payments (
  id INT AUTO_INCREMENT PRIMARY KEY,
  order_id INT NOT NULL,
  payment_method ENUM('credit_card', 'debit_card', 'paypal', 'bank_transfer', 'cash') NOT NULL,
  amount DECIMAL(12, 2) NOT NULL,
  status ENUM('pending', 'completed', 'failed', 'refunded') DEFAULT 'pending',
  transaction_id VARCHAR(100),
  reference_number VARCHAR(100),
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  processed_at DATETIME,
  FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
  INDEX idx_order_id (order_id),
  INDEX idx_status (status),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE reviews (
  id INT AUTO_INCREMENT PRIMARY KEY,
  product_id INT NOT NULL,
  user_id INT NOT NULL,
  rating INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
  title VARCHAR(255),
  comment TEXT,
  is_verified_purchase BOOLEAN DEFAULT FALSE,
  helpful_count INT DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  INDEX idx_product_id (product_id),
  INDEX idx_user_id (user_id),
  INDEX idx_rating (rating),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE coupons (
  id INT AUTO_INCREMENT PRIMARY KEY,
  code VARCHAR(50) NOT NULL UNIQUE,
  description TEXT,
  discount_type ENUM('percentage', 'fixed_amount') NOT NULL,
  discount_value DECIMAL(10, 2) NOT NULL,
  minimum_purchase DECIMAL(10, 2) DEFAULT 0,
  max_uses INT,
  current_uses INT DEFAULT 0,
  is_active BOOLEAN DEFAULT TRUE,
  valid_from DATETIME,
  valid_until DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_code (code),
  INDEX idx_valid_from (valid_from),
  INDEX idx_valid_until (valid_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE wishlist (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  product_id INT NOT NULL,
  added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
  UNIQUE KEY unique_user_product (user_id, product_id),
  INDEX idx_user_id (user_id),
  INDEX idx_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE cart_items (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  product_id INT NOT NULL,
  quantity INT NOT NULL DEFAULT 1,
  added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE CASCADE,
  UNIQUE KEY unique_user_product (user_id, product_id),
  INDEX idx_user_id (user_id),
  INDEX idx_product_id (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE inventory_transactions (
  id INT AUTO_INCREMENT PRIMARY KEY,
  product_id INT NOT NULL,
  transaction_type ENUM('purchase', 'sale', 'return', 'adjustment', 'damage') NOT NULL,
  quantity_change INT NOT NULL,
  reference_id INT,
  reference_type VARCHAR(50),
  notes TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT,
  INDEX idx_product_id (product_id),
  INDEX idx_transaction_type (transaction_type),
  INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- INSERT SAMPLE DATA - ECOMMERCE_DB
-- ============================================================================

START TRANSACTION;

INSERT INTO users (username, email, password_hash, first_name, last_name, phone, date_of_birth, is_active) VALUES
('john_doe', 'john.doe@example.com', 'hash_password_1', 'John', 'Doe', '555-0101', '1990-05-15', TRUE),
('jane_smith', 'jane.smith@example.com', 'hash_password_2', 'Jane', 'Smith', '555-0102', '1992-08-22', TRUE),
('bob_wilson', 'bob.wilson@example.com', 'hash_password_3', 'Bob', 'Wilson', '555-0103', '1988-03-10', TRUE),
('alice_johnson', 'alice.johnson@example.com', 'hash_password_4', 'Alice', 'Johnson', '555-0104', '1995-11-30', TRUE),
('charlie_brown', 'charlie.brown@example.com', 'hash_password_5', 'Charlie', 'Brown', '555-0105', '1991-07-18', TRUE),
('diana_prince', 'diana.prince@example.com', 'hash_password_6', 'Diana', 'Prince', '555-0106', '1993-02-14', TRUE),
('evan_davis', 'evan.davis@example.com', 'hash_password_7', 'Evan', 'Davis', '555-0107', '1989-09-25', TRUE),
('fiona_green', 'fiona.green@example.com', 'hash_password_8', 'Fiona', 'Green', '555-0108', '1994-12-05', TRUE),
('george_harris', 'george.harris@example.com', 'hash_password_9', 'George', 'Harris', '555-0109', '1987-06-12', TRUE),
('hannah_martin', 'hannah.martin@example.com', 'hash_password_10', 'Hannah', 'Martin', '555-0110', '1996-01-20', TRUE);

INSERT INTO user_addresses (user_id, address_type, street_address, city, state_province, postal_code, country, is_default) VALUES
(1, 'billing', '123 Main St', 'New York', 'NY', '10001', 'USA', TRUE),
(1, 'shipping', '456 Oak Ave', 'New York', 'NY', '10002', 'USA', FALSE),
(2, 'billing', '789 Pine Rd', 'Los Angeles', 'CA', '90001', 'USA', TRUE),
(2, 'shipping', '321 Elm St', 'Los Angeles', 'CA', '90002', 'USA', FALSE),
(3, 'billing', '654 Maple Dr', 'Chicago', 'IL', '60601', 'USA', TRUE),
(4, 'billing', '987 Cedar Ln', 'Houston', 'TX', '77001', 'USA', TRUE),
(5, 'billing', '147 Birch Ct', 'Phoenix', 'AZ', '85001', 'USA', TRUE),
(6, 'billing', '258 Spruce Way', 'Philadelphia', 'PA', '19101', 'USA', TRUE),
(7, 'billing', '369 Ash Blvd', 'San Antonio', 'TX', '78201', 'USA', TRUE),
(8, 'billing', '741 Walnut St', 'San Diego', 'CA', '92101', 'USA', TRUE);

INSERT INTO categories (name, description, parent_category_id, is_active) VALUES
('Electronics', 'Electronic devices and gadgets', NULL, TRUE),
('Computers', 'Desktop and laptop computers', 1, TRUE),
('Smartphones', 'Mobile phones and accessories', 1, TRUE),
('Clothing', 'Apparel and fashion items', NULL, TRUE),
('Men Clothing', 'Mens fashion and apparel', 4, TRUE),
('Women Clothing', 'Womens fashion and apparel', 4, TRUE),
('Home & Garden', 'Home and garden products', NULL, TRUE),
('Furniture', 'Furniture and home decor', 7, TRUE),
('Books', 'Books and educational materials', NULL, TRUE),
('Sports', 'Sports equipment and gear', NULL, TRUE);

INSERT INTO products (sku, name, description, category_id, price, cost, stock_quantity, is_active) VALUES
('LAPTOP-001', 'Professional Laptop', 'High-performance laptop for professionals', 2, 1299.99, 800.00, 50, TRUE),
('PHONE-001', 'Smartphone X', 'Latest smartphone with advanced features', 3, 899.99, 500.00, 100, TRUE),
('SHIRT-001', 'Cotton T-Shirt', 'Comfortable cotton t-shirt', 5, 29.99, 10.00, 200, TRUE),
('JEANS-001', 'Blue Denim Jeans', 'Classic blue denim jeans', 5, 79.99, 30.00, 150, TRUE),
('DRESS-001', 'Summer Dress', 'Light summer dress', 6, 59.99, 25.00, 120, TRUE),
('CHAIR-001', 'Office Chair', 'Ergonomic office chair', 8, 249.99, 120.00, 40, TRUE),
('TABLE-001', 'Dining Table', 'Wooden dining table', 8, 399.99, 200.00, 25, TRUE),
('BOOK-001', 'Programming Guide', 'Complete programming guide', 9, 49.99, 15.00, 80, TRUE),
('BALL-001', 'Soccer Ball', 'Professional soccer ball', 10, 34.99, 12.00, 60, TRUE),
('SHOES-001', 'Running Shoes', 'Professional running shoes', 5, 119.99, 50.00, 90, TRUE);

INSERT INTO product_images (product_id, image_url, alt_text, display_order) VALUES
(1, 'https://example.com/laptop1.jpg', 'Laptop front view', 1),
(1, 'https://example.com/laptop2.jpg', 'Laptop side view', 2),
(2, 'https://example.com/phone1.jpg', 'Phone front view', 1),
(3, 'https://example.com/shirt1.jpg', 'T-shirt front', 1),
(4, 'https://example.com/jeans1.jpg', 'Jeans front', 1),
(5, 'https://example.com/dress1.jpg', 'Dress front', 1),
(6, 'https://example.com/chair1.jpg', 'Chair side view', 1),
(7, 'https://example.com/table1.jpg', 'Table top view', 1),
(8, 'https://example.com/book1.jpg', 'Book cover', 1),
(9, 'https://example.com/ball1.jpg', 'Soccer ball', 1);

INSERT INTO orders (order_number, user_id, status, total_amount, tax_amount, shipping_cost, discount_amount, shipping_address_id, billing_address_id) VALUES
('ORD-001', 1, 'delivered', 1329.98, 106.40, 10.00, 0.00, 2, 1),
('ORD-002', 2, 'shipped', 959.98, 76.80, 10.00, 0.00, 4, 3),
('ORD-003', 3, 'confirmed', 109.97, 8.80, 5.00, 0.00, 5, 5),
('ORD-004', 4, 'pending', 449.97, 36.00, 10.00, 0.00, NULL, NULL),
('ORD-005', 5, 'delivered', 649.97, 52.00, 10.00, 0.00, NULL, NULL),
('ORD-006', 6, 'shipped', 299.97, 24.00, 10.00, 0.00, NULL, NULL),
('ORD-007', 7, 'delivered', 179.97, 14.40, 5.00, 0.00, NULL, NULL),
('ORD-008', 8, 'confirmed', 99.97, 8.00, 5.00, 0.00, NULL, NULL),
('ORD-009', 9, 'cancelled', 349.97, 28.00, 10.00, 0.00, NULL, NULL),
('ORD-010', 10, 'delivered', 1229.98, 98.40, 10.00, 0.00, NULL, NULL);

INSERT INTO order_items (order_id, product_id, quantity, unit_price, discount_percent, line_total) VALUES
(1, 1, 1, 1299.99, 0.00, 1299.99),
(1, 3, 1, 29.99, 0.00, 29.99),
(2, 2, 1, 899.99, 0.00, 899.99),
(2, 10, 1, 59.99, 0.00, 59.99),
(3, 3, 2, 29.99, 0.00, 59.98),
(3, 4, 1, 49.99, 0.00, 49.99),
(4, 5, 1, 59.99, 0.00, 59.99),
(4, 6, 1, 249.99, 0.00, 249.99),
(4, 7, 1, 139.99, 0.00, 139.99),
(5, 8, 1, 49.99, 0.00, 49.99);

INSERT INTO payments (order_id, payment_method, amount, status, transaction_id, reference_number) VALUES
(1, 'credit_card', 1446.38, 'completed', 'TXN-001', 'REF-001'),
(2, 'credit_card', 1046.78, 'completed', 'TXN-002', 'REF-002'),
(3, 'debit_card', 163.77, 'completed', 'TXN-003', 'REF-003'),
(4, 'paypal', 515.97, 'pending', 'TXN-004', 'REF-004'),
(5, 'credit_card', 711.97, 'completed', 'TXN-005', 'REF-005'),
(6, 'credit_card', 333.97, 'completed', 'TXN-006', 'REF-006'),
(7, 'debit_card', 199.37, 'completed', 'TXN-007', 'REF-007'),
(8, 'bank_transfer', 112.97, 'pending', 'TXN-008', 'REF-008'),
(9, 'credit_card', 387.97, 'failed', 'TXN-009', 'REF-009'),
(10, 'credit_card', 1338.38, 'completed', 'TXN-010', 'REF-010');

INSERT INTO reviews (product_id, user_id, rating, title, comment, is_verified_purchase, helpful_count) VALUES
(1, 1, 5, 'Excellent laptop', 'Great performance and build quality', TRUE, 15),
(1, 2, 4, 'Good value', 'Good laptop for the price', TRUE, 8),
(2, 3, 5, 'Amazing phone', 'Best phone I have ever used', TRUE, 22),
(3, 4, 4, 'Comfortable shirt', 'Very comfortable to wear', TRUE, 5),
(4, 5, 5, 'Perfect fit', 'Fits perfectly and great quality', TRUE, 12),
(5, 6, 4, 'Nice dress', 'Beautiful dress, great for summer', TRUE, 9),
(6, 7, 5, 'Very comfortable', 'Best office chair ever', TRUE, 18),
(7, 8, 4, 'Solid table', 'Good quality dining table', TRUE, 7),
(8, 9, 5, 'Informative', 'Very helpful programming guide', TRUE, 11),
(9, 10, 5, 'Professional quality', 'Great soccer ball', TRUE, 6);

INSERT INTO coupons (code, description, discount_type, discount_value, minimum_purchase, max_uses, current_uses, is_active, valid_from, valid_until) VALUES
('SAVE10', '10% discount on all items', 'percentage', 10.00, 50.00, 100, 45, TRUE, '2024-01-01', '2024-12-31'),
('FLAT20', '$20 off orders over $100', 'fixed_amount', 20.00, 100.00, 50, 30, TRUE, '2024-01-01', '2024-12-31'),
('WELCOME15', '15% welcome discount', 'percentage', 15.00, 0.00, 1000, 200, TRUE, '2024-01-01', '2024-12-31'),
('SUMMER25', '25% summer sale', 'percentage', 25.00, 75.00, 200, 150, TRUE, '2024-06-01', '2024-08-31'),
('NEWYEAR30', '30% New Year discount', 'percentage', 30.00, 100.00, 100, 80, FALSE, '2024-01-01', '2024-01-31');

INSERT INTO wishlist (user_id, product_id, added_at) VALUES
(1, 2, '2024-01-15 10:30:00'),
(1, 5, '2024-01-16 14:20:00'),
(2, 1, '2024-01-17 09:15:00'),
(3, 6, '2024-01-18 11:45:00'),
(4, 7, '2024-01-19 16:30:00'),
(5, 8, '2024-01-20 13:20:00'),
(6, 9, '2024-01-21 10:00:00'),
(7, 10, '2024-01-22 15:45:00'),
(8, 1, '2024-01-23 12:30:00'),
(9, 3, '2024-01-24 14:15:00');

INSERT INTO cart_items (user_id, product_id, quantity, added_at) VALUES
(1, 1, 1, '2024-01-25 10:00:00'),
(2, 2, 1, '2024-01-25 11:30:00'),
(3, 3, 2, '2024-01-25 12:15:00'),
(4, 4, 1, '2024-01-25 13:45:00'),
(5, 5, 1, '2024-01-25 14:20:00'),
(6, 6, 1, '2024-01-25 15:00:00'),
(7, 7, 1, '2024-01-25 16:30:00'),
(8, 8, 3, '2024-01-25 17:15:00'),
(9, 9, 2, '2024-01-25 18:00:00'),
(10, 10, 1, '2024-01-25 19:30:00');

INSERT INTO inventory_transactions (product_id, transaction_type, quantity_change, reference_id, reference_type, notes) VALUES
(1, 'purchase', 100, NULL, NULL, 'Initial stock purchase'),
(2, 'purchase', 150, NULL, NULL, 'Initial stock purchase'),
(1, 'sale', -1, 1, 'order', 'Sold via order ORD-001'),
(2, 'sale', -1, 2, 'order', 'Sold via order ORD-002'),
(3, 'sale', -2, 3, 'order', 'Sold via order ORD-003'),
(4, 'sale', -1, 3, 'order', 'Sold via order ORD-003'),
(5, 'sale', -1, 4, 'order', 'Sold via order ORD-004'),
(6, 'sale', -1, 4, 'order', 'Sold via order ORD-004'),
(7, 'sale', -1, 4, 'order', 'Sold via order ORD-004'),
(8, 'adjustment', 5, NULL, NULL, 'Stock adjustment');

COMMIT;

-- ============================================================================
-- OPTIONAL ZERO-DATE EXAMPLES (DISABLED BY DEFAULT)
-- Uncomment this block only when validating zero-date precheck behavior.
-- Note: MySQL strict sql_mode may reject these statements unless temporarily relaxed.
-- ============================================================================

-- CREATE DATABASE IF NOT EXISTS legacy_zero_date_db CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
-- USE legacy_zero_date_db;
--
-- CREATE TABLE zero_date_examples (
--   id INT AUTO_INCREMENT PRIMARY KEY,
--   legacy_date DATE NOT NULL DEFAULT '0000-00-00',
--   legacy_datetime DATETIME NOT NULL DEFAULT '0000-00-00 00:00:00',
--   legacy_timestamp TIMESTAMP NOT NULL DEFAULT '0000-00-00 00:00:00',
--   partial_zero_date DATE NOT NULL DEFAULT '2024-00-15',
--   created_at DATETIME DEFAULT CURRENT_TIMESTAMP
-- ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
--
-- INSERT INTO zero_date_examples (
--   legacy_date,
--   legacy_datetime,
--   legacy_timestamp,
--   partial_zero_date
-- ) VALUES (
--   '0000-00-00',
--   '0000-00-00 00:00:00',
--   '1970-01-01 00:00:01',
--   '2024-00-15'
-- );
