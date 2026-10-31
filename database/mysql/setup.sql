-- Database design v1: initialize a NEW, EMPTY catalogdb.
-- Run this entire file ONCE as bootstrap administrator. Stop on any error.
-- MySQL DDL implicitly commits; this setup is not one atomic transaction.
-- Accounts remain LOCKED; set strong passwords before unlocking.
-- Current Catalog startup migration/seed must be separated before using readers.
-- Inspect the result with mysql/verify.sql. See database/README.md.
USE catalogdb;

-- 1. Roles and locked accounts

CREATE ROLE 'catalog_owner_role', 'catalog_read_role', 'recommendations_read_role';

CREATE USER 'catalog_migrator'@'%' REQUIRE SSL ACCOUNT LOCK;
CREATE USER 'catalog_app'@'%' REQUIRE SSL ACCOUNT LOCK;
CREATE USER 'recommendations_app'@'%' REQUIRE SSL ACCOUNT LOCK;

GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES
    ON catalogdb.* TO 'catalog_owner_role';
GRANT 'catalog_owner_role' TO 'catalog_migrator'@'%';
SET DEFAULT ROLE 'catalog_owner_role' TO 'catalog_migrator'@'%';

-- 2. Catalog tables (GORM MySQL v1.5.7, 64-bit Go compatible types)
CREATE TABLE products (
    id VARCHAR(191) NOT NULL,
    name LONGTEXT,
    description LONGTEXT,
    price BIGINT,
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE tags (
    name VARCHAR(191) NOT NULL,
    display_name LONGTEXT,
    PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

CREATE TABLE product_tags (
    product_id VARCHAR(191) NOT NULL,
    tag_name VARCHAR(191) NOT NULL,
    PRIMARY KEY (product_id, tag_name),
    KEY product_tags_tag_product_idx (tag_name, product_id),
    CONSTRAINT fk_product_tags_product FOREIGN KEY (product_id) REFERENCES products (id),
    CONSTRAINT fk_product_tags_tag FOREIGN KEY (tag_name) REFERENCES tags (name)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- 3. Catalog sample data
-- BEGIN GENERATED CATALOG SEED
-- Generated from src/catalog/repository/{products,tags}.json.
-- Regenerate: python database/tools/render_catalog_seed.py
-- Run after Catalog tables exist; never as a runtime reader.
-- Inserts missing rows/links; existing fields are not overwritten.
-- This is an insert-only seed, not a catalog reconciliation/reset.
-- Fresh database: 12 products, 4 tags, 12 links.
USE catalogdb;
SET NAMES utf8mb4;
START TRANSACTION;

INSERT INTO tags (name, display_name) VALUES
    ('accessories', 'Accessories'),
    ('clothing', 'Clothing'),
    ('food', 'Food'),
    ('vehicles', 'Vehicles')
ON DUPLICATE KEY UPDATE name = name;

INSERT INTO products (id, name, description, price) VALUES
    ('cc789f85-1476-452a-8100-9e74502198e0', 'Temporal Tickstopper', 'Stop time for 30 seconds with this vintage-styled pocket watch. Features mechanical wind-up power reserve and temporal disruption failsafe. Includes leather carrying pouch and temporal paradox insurance.', 250),
    ('87e89b11-d319-446d-b9be-50adcca5224a', 'Up & Away Parasol', 'This innocent-looking umbrella conceals a powerful grappling hook system with 50-meter range. Features weather-resistant fabric, built-in compass, and automatic hook retraction. Includes spare hooks and basic parkour instructions.', 125),
    ('4f18544b-70a5-4352-8e19-0d070f46745d', 'Levitator Oxfords', 'Classic Oxford-style shoes concealing cutting-edge anti-gravity technology. Features wall-walking capability, ceiling-escape mode, and auto-stabilization. Available in black or brown. Not recommended for formal dances.', 210),
    ('79bce3f3-935f-4912-8c62-0d2f3e059405', 'Facechanger Formal Wear', 'Transform your appearance instantly with this high-tech bowtie. Features 100 pre-loaded faces, custom face scanning capability, and voice modulation. Battery lasts up to 8 hours on a single charge.', 70),
    ('d27cf49f-b689-4a75-a249-d373e0330bb5', 'The Quiet Quill', 'Control sound waves with this sophisticated pen. Create silence bubbles or emit targeted sonic blasts with simple clicks. Includes premium ink cartridge and electromagnetic interference shield. Actually writes quite smoothly.', 150),
    ('1ca35e86-4b4c-4124-b6b5-076ba4134d0d', 'The Forgetter MK-II', 'These stylish shades pack a powerful amnesia-inducing flash that erases the last 60 seconds of memory from anyone in view. Includes UV protection and auto-darkening lenses. Not recommended for use during important meetings.', 225),
    ('631a3db5-ac07-492c-a994-8cd56923c112', 'The Morning Teleporter', 'Create instant portals to pre-programmed locations with this ceramic marvel. Perfect for quick escapes or coffee runs. Features thermal insulation and spill-proof portal containment. Dishwasher safe on low heat.', 40),
    ('8757729a-c518-4356-8694-9e795a9b3237', 'Forget-Me-Pop', 'This innovative bubblegum creates localized amnesia in your target for 5 minutes per piece. Features three brain-tingling flavors: Forgotten Fruit, Mindwipe Mint, and Blank-Berry. Includes warning label: Do not accidentally pop bubble on yourself.', 20),
    ('d4edfedb-dbe9-4dd9-aae8-009489394955', 'Audio-Illusion Spinner', 'Professional-grade sonic illusion generator disguised as a simple yo-yo. Creates realistic sound effects from footsteps to full orchestras. Includes comprehensive training manual and anti-tangle technology.', 190),
    ('a1258cd2-176c-4507-ade6-746dab5ad625', 'Aqua Ace GT', 'Transform your luxury sports car into a high-speed submarine with the push of a button. Features hydro-jet propulsion, underwater navigation, and oxygen recycling system for up to 8 hours. Includes coral-proof paint coating.', 10000),
    ('d3104128-1d14-4465-99d3-8ab9267c687b', 'SkyCycle X-1000', 'Switch from road to air travel instantly with this cutting-edge motorcycle. Features vertical takeoff capability, stealth mode, and auto-stabilization system. Includes emergency parachute and cloud-navigation GPS.', 9000),
    ('d77f9ae6-e9a8-4a3e-86bd-b72af75cbc49', 'Phantom Pursuit', 'Create perfect duplicates of your vehicle to confuse pursuers. Features multi-angle projection, realistic physics simulation, and remote control capability. Includes tactical evasion manual.', 15000)
ON DUPLICATE KEY UPDATE id = id;

INSERT INTO product_tags (product_id, tag_name) VALUES
    ('cc789f85-1476-452a-8100-9e74502198e0', 'accessories'),
    ('87e89b11-d319-446d-b9be-50adcca5224a', 'clothing'),
    ('4f18544b-70a5-4352-8e19-0d070f46745d', 'clothing'),
    ('79bce3f3-935f-4912-8c62-0d2f3e059405', 'clothing'),
    ('d27cf49f-b689-4a75-a249-d373e0330bb5', 'accessories'),
    ('1ca35e86-4b4c-4124-b6b5-076ba4134d0d', 'accessories'),
    ('631a3db5-ac07-492c-a994-8cd56923c112', 'accessories'),
    ('8757729a-c518-4356-8694-9e795a9b3237', 'food'),
    ('d4edfedb-dbe9-4dd9-aae8-009489394955', 'accessories'),
    ('a1258cd2-176c-4507-ade6-746dab5ad625', 'vehicles'),
    ('d3104128-1d14-4465-99d3-8ab9267c687b', 'vehicles'),
    ('d77f9ae6-e9a8-4a3e-86bd-b72af75cbc49', 'vehicles')
ON DUPLICATE KEY UPDATE product_id = product_id;

COMMIT;
-- END GENERATED CATALOG SEED

-- 4. Runtime reader permissions
GRANT SELECT ON catalogdb.products TO 'catalog_read_role';
GRANT SELECT ON catalogdb.tags TO 'catalog_read_role';
GRANT SELECT ON catalogdb.product_tags TO 'catalog_read_role';
GRANT SELECT ON catalogdb.products TO 'recommendations_read_role';
GRANT SELECT ON catalogdb.product_tags TO 'recommendations_read_role';

GRANT 'catalog_read_role' TO 'catalog_app'@'%';
GRANT 'recommendations_read_role' TO 'recommendations_app'@'%';
SET DEFAULT ROLE 'catalog_read_role' TO 'catalog_app'@'%';
SET DEFAULT ROLE 'recommendations_read_role' TO 'recommendations_app'@'%';
