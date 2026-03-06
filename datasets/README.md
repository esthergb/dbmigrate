# Test Datasets

This directory contains SQL datasets used by the migration matrix test scripts.

## Files

1. `populate_mariadb10.sql` - MariaDB 10.6 compatible baseline dataset.
2. `populate_mariadb11.sql` - MariaDB 11.0 compatible dataset with newer features.
3. `populate_mariadb12.sql` - MariaDB 12.0 compatible dataset with latest MariaDB features.
4. `populate_mysql80.sql` - MySQL 8.0 compatible dataset.
5. `populate_mysql84.sql` - MySQL 8.4 compatible dataset.
6. `phase62_mysql_hidden_schema.sql` - focused MySQL fixture for invisible columns, invisible indexes, and generated invisible primary keys.

## Dataset Design

Each dataset is intended to stress migration behavior with realistic structures:

- multiple application databases (for example `ecommerce_db`, `analytics_db`, `crm_db`, `inventory_db`, `logs_db`)
- relational schemas with PK/FK/index coverage
- varied data types (`INT`, `VARCHAR`, `TEXT`, `DATE`, `DATETIME`, `DECIMAL`, `BOOLEAN`, etc.)
- routines, triggers, and version-appropriate advanced features
- transactional inserts for deterministic seeding

Focused fixtures:

- `phase62_mysql_hidden_schema.sql` is intentionally narrow and should be used by dedicated rehearsals, not by the full baseline matrix.
- It requires a MySQL source because it enables `sql_generate_invisible_primary_key`.

## Usage in Scripts

The matrix runner loads the dataset into the **source** database service before each scenario run.
Destination services are left empty to validate baseline migration behavior.

## Optional Zero-Date Examples

All `populate_*.sql` files now include a commented block named:

- `OPTIONAL ZERO-DATE EXAMPLES (DISABLED BY DEFAULT)`

This block contains sample legacy defaults such as:

- `DATE DEFAULT '0000-00-00'`
- `DATETIME DEFAULT '0000-00-00 00:00:00'`
- `TIMESTAMP DEFAULT '0000-00-00 00:00:00'`
- `DATE DEFAULT 'YYYY-00-DD'`

How to use:

1. Open the dataset file you want to run.
2. Uncomment the optional block.
3. Run the dataset load as usual.

Notes:

- The block is commented by default to keep baseline matrix runs stable.
- MySQL strict `sql_mode` can reject these defaults; this is expected and useful for validating precheck behavior.
