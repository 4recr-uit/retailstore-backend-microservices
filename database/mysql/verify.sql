-- Read-only inspection, as administrator, after schema + seed + grants.
USE catalogdb;
SELECT DATABASE(), CURRENT_USER();

-- Expect three BASE TABLE rows.
SELECT table_name, table_type, engine, table_collation
FROM information_schema.tables WHERE table_schema = DATABASE()
ORDER BY table_name;

SELECT table_name, column_name, column_type, is_nullable, column_default
FROM information_schema.columns WHERE table_schema = DATABASE()
ORDER BY table_name, ordinal_position;

SELECT table_name, index_name, seq_in_index, column_name, non_unique
FROM information_schema.statistics WHERE table_schema = DATABASE()
ORDER BY table_name, index_name, seq_in_index;

-- Fresh seed: 12 products, 4 tags, 12 associations. Existing data can add rows.
SELECT (SELECT COUNT(*) FROM products) AS products,
       (SELECT COUNT(*) FROM tags) AS tags,
       (SELECT COUNT(*) FROM product_tags) AS product_tags;

-- Expect zero; foreign keys also prevent these orphan associations.
SELECT COUNT(*) AS orphan_associations
FROM product_tags pt
LEFT JOIN products p ON p.id = pt.product_id
LEFT JOIN tags t ON t.name = pt.tag_name
WHERE p.id IS NULL OR t.name IS NULL;

SHOW GRANTS FOR 'catalog_owner_role';
SHOW GRANTS FOR 'catalog_read_role';
SHOW GRANTS FOR 'recommendations_read_role';
SHOW GRANTS FOR 'catalog_app'@'%' USING 'catalog_read_role';
SHOW GRANTS FOR 'recommendations_app'@'%' USING 'recommendations_read_role';
