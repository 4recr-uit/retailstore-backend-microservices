-- Database design v1: initialize a NEW, EMPTY retaildb.
-- Run this entire file ONCE as bootstrap administrator (psql -v ON_ERROR_STOP=1).
-- Accounts remain NOLOGIN; provision passwords separately before enabling LOGIN.
-- Current app startup DDL must be separated before using restricted app users.
-- Inspect the result with postgres/verify.sql. See database/README.md.
BEGIN;

-- 1. Roles, schemas and permissions

DO $$
BEGIN
    IF current_database() <> 'retaildb' THEN
        RAISE EXCEPTION 'Connect to retaildb before bootstrapping';
    END IF;
END
$$;

CREATE ROLE cart_owner NOLOGIN;
CREATE ROLE orders_owner NOLOGIN;
CREATE ROLE checkout_owner NOLOGIN;
CREATE ROLE retail_migrator NOLOGIN NOINHERIT;
CREATE ROLE cart_app NOLOGIN;
CREATE ROLE orders_app NOLOGIN;
CREATE ROLE checkout_app NOLOGIN;

-- Owners remain NOLOGIN. The migration identity must SET ROLE explicitly.
GRANT cart_owner, orders_owner, checkout_owner TO retail_migrator;
GRANT cart_owner, orders_owner, checkout_owner TO CURRENT_USER;

REVOKE ALL ON DATABASE retaildb FROM PUBLIC;
GRANT CONNECT ON DATABASE retaildb
    TO retail_migrator, cart_app, orders_app, checkout_app;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

CREATE SCHEMA cart AUTHORIZATION cart_owner;
CREATE SCHEMA orders AUTHORIZATION orders_owner;
CREATE SCHEMA checkout AUTHORIZATION checkout_owner;
REVOKE ALL ON SCHEMA cart, orders, checkout FROM PUBLIC;

GRANT USAGE ON SCHEMA cart TO cart_app;
GRANT USAGE ON SCHEMA orders TO orders_app;
GRANT USAGE ON SCHEMA checkout TO checkout_app;

-- Defaults apply ONLY to objects created by these owner roles.
-- Tables below SET ROLE to the appropriate owner for each schema.
ALTER DEFAULT PRIVILEGES FOR ROLE cart_owner IN SCHEMA cart
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO cart_app;
ALTER DEFAULT PRIVILEGES FOR ROLE cart_owner IN SCHEMA cart
    GRANT USAGE, SELECT ON SEQUENCES TO cart_app;
ALTER DEFAULT PRIVILEGES FOR ROLE orders_owner IN SCHEMA orders
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO orders_app;
ALTER DEFAULT PRIVILEGES FOR ROLE orders_owner IN SCHEMA orders
    GRANT USAGE, SELECT ON SEQUENCES TO orders_app;
ALTER DEFAULT PRIVILEGES FOR ROLE checkout_owner IN SCHEMA checkout
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO checkout_app;

ALTER ROLE cart_app SET search_path = pg_catalog, cart;
ALTER ROLE orders_app SET search_path = pg_catalog, orders;
ALTER ROLE checkout_app SET search_path = pg_catalog, checkout;

-- 2. Eight tables and their indexes

SET LOCAL ROLE cart_owner;

CREATE TABLE cart.carts (
    customer_id VARCHAR(255) PRIMARY KEY,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE cart.cart_items (
    id SERIAL PRIMARY KEY,
    customer_id VARCHAR(255) NOT NULL REFERENCES cart.carts(customer_id) ON DELETE CASCADE,
    item_id VARCHAR(255) NOT NULL,
    quantity INT NOT NULL,
    unit_price INT NOT NULL,
    UNIQUE(customer_id, item_id)
);

CREATE TABLE cart.processed_jobs (
    job_id VARCHAR(64) PRIMARY KEY,
    customer_id VARCHAR(255),
    status VARCHAR(64) DEFAULT 'COMPLETED',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- GetItems filters by customer_id and orders by id.
CREATE INDEX cart_items_customer_id_id_idx ON cart.cart_items (customer_id, id);

SET LOCAL ROLE orders_owner;

CREATE TABLE orders.orders (
    id VARCHAR(255) PRIMARY KEY,
    job_id VARCHAR(64) UNIQUE,
    customer_id VARCHAR(255),
    status VARCHAR(64) DEFAULT 'COMPLETED',
    created_date TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE orders.order_items (
    id SERIAL PRIMARY KEY,
    product_id VARCHAR(255) NOT NULL,
    quantity INT NOT NULL,
    unit_cost INT NOT NULL,
    total_cost INT NOT NULL,
    order_id VARCHAR(255) REFERENCES orders.orders(id) ON DELETE CASCADE
);

CREATE TABLE orders.shipping_addresses (
    order_id VARCHAR(255) PRIMARY KEY REFERENCES orders.orders(id) ON DELETE CASCADE,
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    address1 VARCHAR(255) NOT NULL,
    address2 VARCHAR(255),
    city VARCHAR(255) NOT NULL,
    zip_code VARCHAR(255) NOT NULL,
    state_id VARCHAR(255) NOT NULL
);

CREATE INDEX orders_created_date_idx ON orders.orders (created_date DESC);
CREATE INDEX order_items_order_id_idx ON orders.order_items (order_id);

SET LOCAL ROLE checkout_owner;

CREATE TABLE checkout.checkout_sessions (
    customer_id VARCHAR(255) PRIMARY KEY,
    data JSONB NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE checkout.processed_jobs (
    job_id VARCHAR(64) PRIMARY KEY,
    order_id VARCHAR(255) NOT NULL,
    customer_id VARCHAR(255),
    status VARCHAR(64) DEFAULT 'COMPLETED',
    data JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

COMMIT;
