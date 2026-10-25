CREATE TABLE customers (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100) NOT NULL,
  email VARCHAR(150) NOT NULL UNIQUE,
  city VARCHAR(50),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  note TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO customers (name, email, city, note) VALUES
  ('Customer 001','c1@example.test','Bandung','catatan 1'),
  ('Customer 002','c2@example.test','Jakarta',NULL),
  ('Customer 003','c3@example.test','Medan','');
CREATE INDEX customers_city_idx ON customers (city);

CREATE TABLE orders (
  id INT AUTO_INCREMENT PRIMARY KEY,
  customer_id INT NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'new',
  CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id),
  CONSTRAINT orders_status_chk CHECK (status IN ('new','paid','shipped','cancelled'))
);
INSERT INTO orders (customer_id, status) VALUES (1,'new'),(2,'paid'),(3,'shipped');

CREATE VIEW order_totals AS SELECT customer_id, COUNT(*) AS n FROM orders GROUP BY customer_id;

CREATE TABLE legacy_notes (author VARCHAR(50), body TEXT);
INSERT INTO legacy_notes VALUES ('andi','pendek'),(NULL,'<b>HTML</b> & "khusus"');