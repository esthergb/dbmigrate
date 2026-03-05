# Test Datasets

This directory contains SQL datasets used by the migration matrix test scripts.

## Files

1. `populate_mariadb10.sql` - MariaDB 10.6 compatible baseline dataset.
2. `populate_mariadb11.sql` - MariaDB 11.0 compatible dataset with newer features.
3. `populate_mariadb12.sql` - MariaDB 12.0 compatible dataset with latest MariaDB features.
4. `populate_mysql80.sql` - MySQL 8.0 compatible dataset.
5. `populate_mysql84.sql` - MySQL 8.4 compatible dataset.

## Dataset Design

Each dataset is intended to stress migration behavior with realistic structures:

- multiple application databases (for example `ecommerce_db`, `analytics_db`, `crm_db`, `inventory_db`, `logs_db`)
- relational schemas with PK/FK/index coverage
- varied data types (`INT`, `VARCHAR`, `TEXT`, `DATE`, `DATETIME`, `DECIMAL`, `BOOLEAN`, etc.)
- routines, triggers, and version-appropriate advanced features
- transactional inserts for deterministic seeding

## Usage in Scripts

The matrix runner loads the dataset into the **source** database service before each scenario run.
Destination services are left empty to validate baseline migration behavior.
