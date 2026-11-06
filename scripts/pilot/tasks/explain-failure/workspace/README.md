# pickpath

Builds pick sheets for the warehouse floor: given one order (or a wave of
several orders), it lists the stops a picker makes, zone by zone, in walking
order. Standard library only.

## Location labels

Rack labels look like `B-07-3`: zone `B`, aisle `07`, bay `3`. Bays are
numbered from 1 on the labels.

Pickers visit zones in the order `R, A, B, C, D, M` (anything else comes last,
alphabetically). Inside a zone they walk the aisles in a serpentine path: odd
aisles front to back (bay 1 first), even aisles back to front.

## Usage

```python
from warehouse import Order, OrderLine, pick_sheet, render

order = Order("SO-1001", [OrderLine("MUG-12", 2, "C-03-1"), OrderLine("TEA-01", 1, "A-01-4")])
print(render(pick_sheet(order), title="SO-1001"))
```

`build_wave(orders)` does the same for several orders at once, merging their
lines zone by zone.

## Tests

    python3 -m unittest discover -s tests -v

## Layout

- `warehouse/models.py` - orders, order lines and location labels
- `warehouse/picklist.py` - grouping by zone and walking order
- `warehouse/report.py` - plain-text pick sheet
- `tests/` - unit tests
