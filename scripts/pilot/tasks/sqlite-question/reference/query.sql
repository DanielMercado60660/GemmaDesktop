-- Top customer by money spent in 2025, excluding cancelled orders.
-- order_date holds 'YYYY-MM-DD HH:MM:SS' text, so use a half-open range:
-- BETWEEN '2025-01-01' AND '2025-12-31' would miss orders placed later on Dec 31.
SELECT c.id   AS customer_id,
       c.name AS customer,
       ROUND(SUM(oi.quantity * oi.unit_price), 2) AS total_spent
FROM orders o
JOIN customers c    ON c.id = o.customer_id
JOIN order_items oi ON oi.order_id = o.id
WHERE o.status <> 'cancelled'
  AND o.order_date >= '2025-01-01'
  AND o.order_date <  '2026-01-01'
GROUP BY c.id, c.name
ORDER BY total_spent DESC
LIMIT 1;
