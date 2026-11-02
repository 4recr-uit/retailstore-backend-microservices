-- Read-only inspection. Run after postgres/setup.sql; compare with README.md.
SELECT current_database(), current_user;

-- Expect eight BASE TABLE rows, each with table_exists = true.
WITH expected(schema_name, table_name) AS (
    VALUES ('cart', 'carts'), ('cart', 'cart_items'), ('cart', 'processed_jobs'),
           ('orders', 'orders'), ('orders', 'order_items'), ('orders', 'shipping_addresses'),
           ('checkout', 'checkout_sessions'), ('checkout', 'processed_jobs')
)
SELECT e.*, t.table_type = 'BASE TABLE' AS table_exists
FROM expected e
LEFT JOIN information_schema.tables t
    ON t.table_schema = e.schema_name AND t.table_name = e.table_name
ORDER BY e.schema_name, e.table_name;

SELECT schemaname, tablename, tableowner
FROM pg_tables WHERE schemaname IN ('cart', 'orders', 'checkout')
ORDER BY schemaname, tablename;

SELECT table_schema, table_name, column_name, data_type, is_nullable, column_default
FROM information_schema.columns WHERE table_schema IN ('cart', 'orders', 'checkout')
ORDER BY table_schema, table_name, ordinal_position;

SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes WHERE schemaname IN ('cart', 'orders', 'checkout')
ORDER BY schemaname, tablename, indexname;

-- Expect true for own-schema usage and false for cross-schema usage / creation.
SELECT has_schema_privilege('cart_app', 'cart', 'USAGE') AS cart_can_use_cart,
       has_schema_privilege('cart_app', 'orders', 'USAGE') AS cart_can_use_orders,
       has_schema_privilege('orders_app', 'checkout', 'USAGE') AS orders_can_use_checkout,
       has_schema_privilege('checkout_app', 'cart', 'USAGE') AS checkout_can_use_cart,
       has_schema_privilege('cart_app', 'cart', 'CREATE') AS cart_can_create,
       has_schema_privilege('orders_app', 'orders', 'CREATE') AS orders_can_create,
       has_schema_privilege('checkout_app', 'checkout', 'CREATE') AS checkout_can_create;

-- SERIAL inserts require sequence USAGE in addition to table INSERT.
SELECT has_sequence_privilege('cart_app', 'cart.cart_items_id_seq', 'USAGE') AS cart_sequence_usage,
       has_sequence_privilege('orders_app', 'orders.order_items_id_seq', 'USAGE') AS orders_sequence_usage;

SELECT rolname, rolcanlogin, rolsuper, rolcreatedb, rolcreaterole
FROM pg_roles
WHERE rolname IN ('cart_owner', 'orders_owner', 'checkout_owner', 'retail_migrator',
                  'cart_app', 'orders_app', 'checkout_app')
ORDER BY rolname;
