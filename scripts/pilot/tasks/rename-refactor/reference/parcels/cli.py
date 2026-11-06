"""Command line quotes.

    python3 -m parcels.cli WEIGHT_KG ZIP [--express]
    python3 -m parcels.cli WEIGHT_KG --table [--express]
"""
import argparse
import sys

from .quotes import quote_table
from .rates import shipping_cost, zone_for


def main(argv=None, out=None):
    out = out or sys.stdout
    parser = argparse.ArgumentParser(prog="parcels", description="Quote parcel shipping.")
    parser.add_argument("weight", help="parcel weight in kg")
    parser.add_argument("zip", nargs="?", help="destination ZIP code")
    parser.add_argument("--express", action="store_true", help="express service")
    parser.add_argument("--table", action="store_true", help="show every zone")
    args = parser.parse_args(argv)
    service = "express" if args.express else "standard"
    try:
        if args.table:
            for zone, price in quote_table(args.weight, service).items():
                print(f"zone {zone}: ${price}", file=out)
            return 0
        if not args.zip:
            parser.error("a ZIP code is required unless --table is given")
        zone = zone_for(args.zip)
        price = shipping_cost(args.weight, zone, express=args.express)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        return 2
    print(f"{args.weight} kg to {args.zip} (zone {zone}, {service}): ${price}", file=out)
    return 0


if __name__ == "__main__":
    sys.exit(main())
