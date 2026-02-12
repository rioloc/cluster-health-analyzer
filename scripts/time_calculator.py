#!/usr/bin/env python3
"""
Calculate timestamps for different time periods relative to now.
Outputs both Unix epoch time and formatted dates.
"""

from datetime import datetime, timedelta


def calculate_timestamps():
    """Calculate timestamps for various time periods ago."""
    now = datetime.now()

    periods = [("now", 0)] + [(f"now-{d}d", d) for d in range(1, 16)]

    print("=" * 80)
    print(f"Current time: {now.strftime('%Y-%m-%d %H:%M:%S')}")
    print("=" * 80)
    print()

    for label, days in periods:
        timestamp_dt = now - timedelta(days=days)
        unix_epoch = int(timestamp_dt.timestamp())
        formatted_date = timestamp_dt.strftime('%Y-%m-%d %H:%M:%S')
        minutes = days * 24 * 60

        print(f"{label:10s} | Unix Epoch: {unix_epoch:12d} | Date: {formatted_date} | Minutes ago: {minutes:6d}")

    print()
    print("=" * 80)


if __name__ == "__main__":
    calculate_timestamps()
