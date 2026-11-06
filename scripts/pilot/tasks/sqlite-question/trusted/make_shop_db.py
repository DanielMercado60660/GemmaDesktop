#!/usr/bin/env python3
"""Deterministic generator for the sqlite-question fixture (checker-side only).

Writes three byte-stable SQLite files:
  workspace/data/shop.db   what the model sees
  trusted/data/shop.db     identical copy the checker trusts
  trusted/variant/shop.db  same schema and traps, different data and a
                           different correct answer (proves query.sql really
                           computes the answer instead of hard-coding it)

No randomness from the stdlib: a fixed LCG keeps output identical across
Python versions. Each build asserts its traps (see INVARIANTS below).
"""
from __future__ import annotations

from datetime import date, timedelta
from pathlib import Path
import sqlite3

HERE = Path(__file__).resolve().parent
TASK = HERE.parent

CUSTOMERS = [
    (1, "Grace Okafor", "grace.okafor@example.com", "Portland", "2023-04-11"),
    (2, "Daniel Brooks", "dbrooks@example.com", "Austin", "2023-06-02"),
    (3, "Priya Raman", "priya.raman@example.com", "Seattle", "2023-08-19"),
    (4, "Marcus Lindqvist", "marcus.l@example.com", "Minneapolis", "2023-09-30"),
    (5, "Hannah Kim", "hannah.kim@example.com", "San Diego", "2024-01-14"),
    (6, "Tomasz Nowak", "tnowak@example.com", "Chicago", "2024-02-08"),
    (7, "Elena Petrova", "elena.petrova@example.com", "Boston", "2024-03-21"),
    (8, "Samuel Adeyemi", "sam.adeyemi@example.com", "Atlanta", "2024-04-05"),
    (9, "Lucia Romero", "lucia.romero@example.com", "Denver", "2024-05-17"),
    (10, "Owen Fitzgerald", "owen.fitz@example.com", "Philadelphia", "2024-06-03"),
    (11, "Aisha Rahman", "aisha.rahman@example.com", "Houston", "2024-07-22"),
    (12, "Bennett Cole", "bennett.cole@example.com", "Raleigh", "2024-08-30"),
]

PRODUCTS = [
    ("USB-C Charger 65W", 39.99), ("Wireless Mouse", 24.50), ("Mechanical Keyboard", 89.00),
    ("27in Monitor", 249.99), ("Laptop Stand", 34.95), ("Noise-Cancelling Headphones", 179.00),
    ("HDMI Cable 2m", 12.49), ("Webcam 1080p", 59.90), ("Desk Lamp", 42.00),
    ("External SSD 1TB", 109.99), ("Cable Organizer", 9.75), ("Portable Speaker", 64.50),
    ("Mouse Pad XL", 18.25), ("Docking Station", 149.00), ("Ergonomic Chair", 329.00),
]
P = dict(PRODUCTS)

START, END = date(2024, 7, 1), date(2026, 1, 9)  # data snapshot taken 2026-01-10


class LCG:
    def __init__(self, seed: int):
        self.x = seed & 0xFFFFFFFF

    def next(self) -> int:
        self.x = (1664525 * self.x + 1013904223) & 0xFFFFFFFF
        return self.x >> 8

    def below(self, n: int) -> int:
        return self.next() % n


def ts(d: date, rng: LCG, lo_hour: int = 7, hi_hour: int = 23) -> str:
    return f"{d.isoformat()} {lo_hour + rng.below(hi_hour - lo_hour):02d}:{rng.below(60):02d}:{rng.below(60):02d}"


def base_orders(seed: int) -> list[tuple]:
    """Ordinary background orders: (customer_id, timestamp, status, [(product, qty, price)])."""
    rng = LCG(seed)
    days = (END - START).days + 1
    out = []
    for cid, *_ in CUSTOMERS:
        n = 6 + rng.below(4)
        for _ in range(n):
            d = START + timedelta(days=rng.below(days))
            items = []
            for _ in range(1 + rng.below(3)):
                name, price = PRODUCTS[rng.below(len(PRODUCTS) - 1)]  # chair only in trap orders
                items.append((name, 1 + rng.below(2) if price < 100 else 1, price))
            if d >= date(2025, 12, 22):
                status = "shipped"
            else:
                status = "cancelled" if rng.below(100) < 5 else "delivered"
            out.append((cid, ts(d, rng), status, items))
    return out


def fill(cid: int, amount: float, stamps: list[str]) -> list[tuple]:
    """Delivered orders for cid on the given timestamps worth about `amount` (within 10)."""
    assert amount > 150, (cid, amount)
    lines, remaining = [], amount
    for name, price in sorted(PRODUCTS, key=lambda p: -p[1]):
        q = min(int(remaining // price), 2)
        if q:
            lines.append((name, q, price))
            remaining -= q * price
    assert remaining < 10, (cid, amount, remaining)
    orders = [[] for _ in stamps]
    for k, line in enumerate(lines):
        orders[k % len(stamps)].append(line)
    return [(cid, when, "delivered", items) for when, items in zip(stamps, orders) if items]


def spent_2025(orders: list[tuple], cid: int) -> float:
    return sum(q * p for c, when, status, items in orders if c == cid and when.startswith("2025") and status != "cancelled" for _, q, p in items)


def trap_orders(roles: dict[str, int], base: list[tuple]) -> list[tuple]:
    """Orders that make each plausible mistake produce a different answer."""
    T, R, X, Y, D = roles["T"], roles["R"], roles["X"], roles["Y"], roles["D"]
    o = []
    # T: true 2025 top spender. Its biggest order lands on New Year's Eve in the
    # evening, so BETWEEN '2025-01-01' AND '2025-12-31' silently drops it.
    o += [
        (T, "2025-02-14 10:22:41", "delivered", [("27in Monitor", 2, P["27in Monitor"]), ("HDMI Cable 2m", 2, P["HDMI Cable 2m"])]),
        (T, "2025-05-03 16:05:12", "delivered", [("Ergonomic Chair", 1, P["Ergonomic Chair"]), ("Desk Lamp", 1, P["Desk Lamp"])]),
        (T, "2025-08-27 09:48:30", "delivered", [("Docking Station", 2, P["Docking Station"]), ("External SSD 1TB", 1, P["External SSD 1TB"])]),
        (T, "2025-10-09 13:31:57", "cancelled", [("Noise-Cancelling Headphones", 1, P["Noise-Cancelling Headphones"])]),
        (T, "2025-12-31 19:42:10", "shipped", [("Ergonomic Chair", 2, P["Ergonomic Chair"]), ("27in Monitor", 2, 239.99), ("Webcam 1080p", 1, P["Webcam 1080p"])]),
        (T, "2024-12-31 22:57:03", "delivered", [("Noise-Cancelling Headphones", 1, P["Noise-Cancelling Headphones"]), ("Laptop Stand", 1, P["Laptop Stand"])]),
        (T, "2026-01-01 00:14:26", "shipped", [("Mechanical Keyboard", 1, P["Mechanical Keyboard"]), ("Wireless Mouse", 1, P["Wireless Mouse"])]),
    ]
    top = spent_2025(base + o, T)
    # R: runner-up, about 450 behind; tops the list only if T's New Year's Eve order is dropped.
    o += fill(R, top - 450 - spent_2025(base, R), ["2025-03-08 11:15:09", "2025-06-21 15:40:44", "2025-09-12 08:27:18", "2025-11-28 19:03:51"])
    # X: about 700 behind, but would win if its big cancelled order were counted.
    o += fill(X, top - 700 - spent_2025(base, X), ["2025-04-17 14:12:33", "2025-07-30 10:51:06"])
    o.append((X, "2025-11-05 17:36:22", "cancelled", [("Ergonomic Chair", 4, P["Ergonomic Chair"]), ("27in Monitor", 4, P["27in Monitor"])]))
    # Y: modest in 2025, but would win if every year were counted.
    o += fill(Y, top - 1600 - spent_2025(base, Y), ["2025-06-02 18:44:10"])
    o += [
        (Y, "2024-09-14 12:08:45", "delivered", [("Ergonomic Chair", 4, P["Ergonomic Chair"]), ("27in Monitor", 4, P["27in Monitor"])]),
        (Y, "2024-11-29 09:19:37", "delivered", [("Docking Station", 4, P["Docking Station"]), ("Noise-Cancelling Headphones", 3, P["Noise-Cancelling Headphones"])]),
        (Y, "2026-01-05 11:27:52", "shipped", [("Ergonomic Chair", 2, P["Ergonomic Chair"]), ("27in Monitor", 2, P["27in Monitor"])]),
    ]
    # D: the most orders in 2025, all of them small.
    for i, (md, prod) in enumerate([("01-09", "Cable Organizer"), ("02-02", "HDMI Cable 2m"), ("03-15", "Mouse Pad XL"),
                                    ("04-26", "Wireless Mouse"), ("06-07", "Cable Organizer"), ("07-19", "HDMI Cable 2m"),
                                    ("08-30", "USB-C Charger 65W"), ("10-11", "Mouse Pad XL"), ("11-22", "Wireless Mouse")]):
        o.append((D, f"2025-{md} {9 + i:02d}:{(17 * i) % 60:02d}:{(29 * i) % 60:02d}", "delivered", [(prod, 2, P[prod])]))
    return o


SCHEMA = """
CREATE TABLE customers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    city TEXT,
    joined_on TEXT NOT NULL
);
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL REFERENCES customers(id),
    order_date TEXT NOT NULL,
    status TEXT NOT NULL
);
CREATE TABLE order_items (
    id INTEGER PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    product TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL
);
CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_items_order ON order_items(order_id);
"""

# Each entry: (description, SQL) - every one must name a different customer
# than the truth, or give the truth customer a different amount.
TOTAL = "SUM(i.quantity * i.unit_price)"
JOIN = "FROM customers c JOIN orders o ON o.customer_id = c.id JOIN order_items i ON i.order_id = o.id"
TRUTH_SQL = f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.status <> 'cancelled' AND o.order_date >= '2025-01-01' AND o.order_date < '2026-01-01' GROUP BY c.id ORDER BY spent DESC"
WRONG = {
    "cancelled included": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.order_date >= '2025-01-01' AND o.order_date < '2026-01-01' GROUP BY c.id ORDER BY spent DESC",
    "all years": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.status <> 'cancelled' GROUP BY c.id ORDER BY spent DESC",
    "BETWEEN drops Dec 31": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.status <> 'cancelled' AND o.order_date BETWEEN '2025-01-01' AND '2025-12-31' GROUP BY c.id ORDER BY spent DESC",
    "delivered only": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.status = 'delivered' AND o.order_date >= '2025-01-01' AND o.order_date < '2026-01-01' GROUP BY c.id ORDER BY spent DESC",
    "includes 2024-12-31 and 2026-01-01": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} WHERE o.status <> 'cancelled' AND o.order_date >= '2024-12-31' AND o.order_date < '2026-01-02' GROUP BY c.id ORDER BY spent DESC",
    "nothing filtered": f"SELECT c.id, c.name, ROUND({TOTAL}, 2) AS spent {JOIN} GROUP BY c.id ORDER BY spent DESC",
}


def build(path: Path, seed: int, roles: dict[str, int]) -> dict:
    if path.exists():
        path.unlink()
    base = base_orders(seed)
    orders = sorted(base + trap_orders(roles, base), key=lambda o: (o[1], o[0]))
    con = sqlite3.connect(path)
    con.execute("PRAGMA page_size = 1024")  # keeps the fixture small
    con.executescript(SCHEMA)
    con.executemany("INSERT INTO customers VALUES (?, ?, ?, ?, ?)", CUSTOMERS)
    item_id = 1
    for n, (cid, when, status, items) in enumerate(orders):
        oid = 1001 + n
        con.execute("INSERT INTO orders VALUES (?, ?, ?, ?)", (oid, cid, when, status))
        for product, qty, price in items:
            con.execute("INSERT INTO order_items VALUES (?, ?, ?, ?, ?)", (item_id, oid, product, qty, price))
            item_id += 1
    con.commit()
    con.execute("VACUUM")
    con.close()
    return verify(path, roles)


def verify(path: Path, roles: dict[str, int]) -> dict:
    con = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    truth = con.execute(TRUTH_SQL).fetchall()
    top_id, top_name, top_amount = truth[0]
    assert top_id == roles["T"], (path, truth[:3])
    assert truth[1][2] < top_amount - 50, "clear winner"
    assert all(abs(a - top_amount) > 1 for _, _, a in truth[1:]), "no near-tie amounts"
    expect_top = {"cancelled included": roles["X"], "all years": roles["Y"], "BETWEEN drops Dec 31": roles["R"]}
    for label, sql in WRONG.items():
        rows = con.execute(sql).fetchall()
        t_amount = next((a for cid, _, a in rows if cid == roles["T"]), None)
        assert t_amount is None or abs(t_amount - top_amount) > 1, (label, t_amount)
        if label in expect_top:
            assert rows[0][0] == expect_top[label], (label, rows[:3])
    counts = con.execute("SELECT customer_id, COUNT(*) n FROM orders WHERE status <> 'cancelled' AND order_date LIKE '2025%' GROUP BY customer_id ORDER BY n DESC").fetchall()
    assert counts[0][0] == roles["D"] and counts[1][1] < counts[0][1], counts[:3]
    stats = {
        "orders": con.execute("SELECT COUNT(*) FROM orders").fetchone()[0],
        "items": con.execute("SELECT COUNT(*) FROM order_items").fetchone()[0],
        "cancelled": con.execute("SELECT COUNT(*) FROM orders WHERE status = 'cancelled'").fetchone()[0],
        "statuses": con.execute("SELECT group_concat(DISTINCT status) FROM orders").fetchone()[0],
    }
    con.close()
    return {"top": (top_id, top_name, top_amount), "next": truth[1], "stats": stats}


MAIN = dict(seed=20250117, roles={"T": 9, "R": 4, "X": 3, "Y": 10, "D": 2})
VARIANT = dict(seed=777013, roles={"T": 7, "R": 12, "X": 5, "Y": 1, "D": 8})


def main():
    info = build(TASK / "trusted" / "data" / "shop.db", **MAIN)
    (TASK / "workspace" / "data").mkdir(parents=True, exist_ok=True)
    (TASK / "workspace" / "data" / "shop.db").write_bytes((TASK / "trusted" / "data" / "shop.db").read_bytes())
    vinfo = build(TASK / "trusted" / "variant" / "shop.db", **VARIANT)
    print("main:", info)
    print("variant:", vinfo)


if __name__ == "__main__":
    main()
