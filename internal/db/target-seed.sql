-- Seed untuk target DB sampel (docker-compose service "target-db", port 5435).

CREATE TABLE customers (
	id serial PRIMARY KEY,
	name text NOT NULL,
	email text NOT NULL UNIQUE,
	city text,
	is_active boolean NOT NULL DEFAULT true,
	note text,
	created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO customers (name, email, city, is_active, note, created_at)
SELECT
	'Customer ' || lpad(g::text, 3, '0'),
	'customer' || g || '@example.test',
	(ARRAY['Bandung', 'Jakarta', 'Surabaya', 'Medan', 'Yogyakarta'])[1 + g % 5],
	g % 7 <> 0,
	CASE WHEN g % 10 = 0 THEN NULL WHEN g % 25 = 0 THEN '' ELSE 'catatan ' || g END,
	timestamptz '2026-01-01 08:00:00+07' + (g || ' hours')::interval
FROM generate_series(1, 200) AS g;

CREATE INDEX customers_city_idx ON customers (city);

CREATE TABLE products (
	id serial PRIMARY KEY,
	sku text NOT NULL UNIQUE,
	name text NOT NULL,
	price numeric(10, 2) NOT NULL CHECK (price >= 0),
	stock integer NOT NULL DEFAULT 0 CHECK (stock >= 0),
	tags text[]
);

INSERT INTO products (sku, name, price, stock, tags)
SELECT
	'SKU-' || lpad(g::text, 4, '0'),
	'Produk ' || g,
	(g * 12500 + 990)::numeric / 100,
	(g * 7) % 50,
	CASE WHEN g % 3 = 0 THEN NULL ELSE ARRAY['tag' || g % 4, 'grup' || g % 3] END
FROM generate_series(1, 30) AS g;

CREATE TABLE orders (
	id serial PRIMARY KEY,
	customer_id integer NOT NULL REFERENCES customers (id),
	status text NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'paid', 'shipped', 'cancelled')),
	ordered_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO orders (customer_id, status, ordered_at)
SELECT
	1 + (g * 7) % 200,
	(ARRAY['new', 'paid', 'shipped', 'cancelled'])[1 + g % 4],
	timestamptz '2026-03-01 09:00:00+07' + (g * 5 || ' hours')::interval
FROM generate_series(1, 120) AS g;

CREATE INDEX orders_customer_idx ON orders (customer_id);
CREATE INDEX orders_status_ordered_idx ON orders (status, ordered_at DESC);

CREATE TABLE order_items (
	order_id integer NOT NULL REFERENCES orders (id) ON DELETE CASCADE,
	product_id integer NOT NULL REFERENCES products (id),
	qty integer NOT NULL CHECK (qty > 0),
	unit_price numeric(10, 2) NOT NULL,
	PRIMARY KEY (order_id, product_id)
);

INSERT INTO order_items (order_id, product_id, qty, unit_price)
SELECT o.id, p.id, 1 + (o.id + p.id) % 4, p.price
FROM orders o
JOIN products p ON p.id IN (1 + o.id % 30, 1 + (o.id * 3) % 30);

CREATE VIEW order_totals AS
SELECT o.id AS order_id, o.customer_id, o.status, sum(i.qty * i.unit_price) AS total
FROM orders o
JOIN order_items i ON i.order_id = o.id
GROUP BY o.id, o.customer_id, o.status;

-- Sengaja tanpa primary key: viewer harus memakai urutan ctid.
CREATE TABLE legacy_notes (
	author text,
	body text
);

INSERT INTO legacy_notes (author, body) VALUES
	('andi', 'catatan pendek'),
	('budi', ''),
	(NULL, 'penulis tidak diketahui'),
	('citra', '<b>tag HTML</b> & karakter "khusus" harus tampil apa adanya'),
	('dewi', repeat('teks sangat panjang ', 100));

CREATE TABLE "Legacy Reports" (
	"Report ID" serial PRIMARY KEY,
	"Report Title" text NOT NULL
);

INSERT INTO "Legacy Reports" ("Report Title") VALUES ('Laporan Q1'), ('Laporan Q2'), ('Laporan "Tahunan"');

CREATE SCHEMA analytics;

CREATE TABLE analytics.events (
	id bigserial PRIMARY KEY,
	customer_id integer REFERENCES public.customers (id),
	kind text NOT NULL,
	payload jsonb,
	occurred_at timestamptz NOT NULL
);

INSERT INTO analytics.events (customer_id, kind, payload, occurred_at)
SELECT
	CASE WHEN g % 9 = 0 THEN NULL ELSE 1 + g % 200 END,
	(ARRAY['login', 'view', 'purchase'])[1 + g % 3],
	CASE WHEN g % 4 = 0 THEN NULL ELSE jsonb_build_object('n', g, 'ok', g % 2 = 0) END,
	timestamptz '2026-04-01 00:00:00+07' + (g || ' minutes')::interval
FROM generate_series(1, 250) AS g;

CREATE INDEX events_occurred_idx ON analytics.events (occurred_at);
CREATE INDEX events_kind_idx ON analytics.events (kind);
