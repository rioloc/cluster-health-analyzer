#!/usr/bin/env python3
"""
Alert Timeline Visualizer for cluster-health-analyzer

Parses CSV or OpenMetrics format files and renders alerts as horizontal bars
showing firing periods over time. Each unique alert stays on its own line,
with gaps displayed where the alert was not firing.

Supported formats:
- CSV: testdata scenario format (start,end,alertname,namespace,severity,silenced,labels)
- OpenMetrics: ALERTS metric format from cluster-health-analyzer output
"""

import argparse
import csv
import json
import re
import sys
from collections import defaultdict
from datetime import datetime, timedelta
from typing import Dict, List, Tuple, Optional

import matplotlib.pyplot as plt
import matplotlib.dates as mdates
from matplotlib.patches import Rectangle
import numpy as np


class Alert:
    """Represents a unique alert with its firing timeline"""

    def __init__(self, alertname: str, labels: Dict[str, str]):
        self.alertname = alertname
        self.labels = labels
        self.timestamps = []
        self.segments = []  # Direct segments for CSV input
        self.severity = labels.get('severity', 'info')
        self.namespace = labels.get('namespace', 'unknown')

    def add_timestamp(self, ts: int):
        """Add a firing timestamp (for OpenMetrics format)"""
        self.timestamps.append(ts)

    def add_segment(self, start: int, end: int):
        """Add a direct time segment (for CSV format)"""
        self.segments.append((start, end))

    def get_label(self) -> str:
        """Generate a readable label for the alert"""
        extra_labels = []
        for key, value in sorted(self.labels.items()):
            if key not in ['alertname', 'namespace', 'severity', 'silenced', 'alertstate']:
                if value:  # Only include non-empty values
                    extra_labels.append(f"{key}={value}")

        if extra_labels:
            return f"{self.alertname} ({self.namespace}) [{', '.join(extra_labels)}]"
        return f"{self.alertname} ({self.namespace})"

    def get_unique_key(self) -> str:
        """Generate a unique key from alertname and relevant labels"""
        # Sort labels for consistent key generation
        label_parts = [f"{k}={v}" for k, v in sorted(self.labels.items())]
        return f"{self.alertname}|{';'.join(label_parts)}"

    def get_timeline_segments(self, tolerance_seconds: int = 600) -> List[Tuple[int, int]]:
        """
        Get timeline segments.

        For CSV format: returns direct segments
        For OpenMetrics format: converts timestamps to continuous segments

        Args:
            tolerance_seconds: Maximum gap between timestamps to consider continuous
                             (only used for OpenMetrics format)

        Returns:
            List of (start_timestamp, end_timestamp) tuples
        """
        # If we have direct segments (from CSV), return them
        if self.segments:
            return sorted(self.segments)

        # Otherwise, convert timestamps to segments (from OpenMetrics)
        if not self.timestamps:
            return []

        sorted_ts = sorted(self.timestamps)
        segments = []
        segment_start = sorted_ts[0]
        prev_ts = sorted_ts[0]

        for ts in sorted_ts[1:]:
            # If gap is larger than tolerance, start new segment
            if ts - prev_ts > tolerance_seconds:
                segments.append((segment_start, prev_ts))
                segment_start = ts
            prev_ts = ts

        # Add final segment
        segments.append((segment_start, prev_ts))

        return segments


def parse_csv_file(filepath: str, base_time: Optional[datetime] = None) -> Tuple[Dict[str, Alert], Optional[int]]:
    """
    Parse CSV scenario file and extract alerts.

    CSV Format (testdata):
    start,end,alertname,namespace,severity,silenced,labels

    Note: start/end are in MINUTES (not seconds) as per simulate logic.
    The simulate command calculates: absStart = now - maxEnd, then each alert
    runs from absStart + start to absStart + end.

    Args:
        filepath: Path to the CSV file
        base_time: Base datetime for scenario end (default: now)

    Returns:
        Tuple of (alerts dict, Watchdog max timestamp or None)
    """
    alerts = {}
    intervals = []

    if base_time is None:
        base_time = datetime.now()

    # First pass: read all intervals and find maxEnd
    with open(filepath, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            try:
                start_minutes = int(row['start'])
                end_minutes = int(row['end'])
                alertname = row['alertname']
                namespace = row['namespace']
                severity = row['severity']
                silenced = row['silenced'].lower() == 'true'

                # Parse additional labels if present
                extra_labels = {}
                labels_str = row.get('labels', '').strip()
                if labels_str:
                    try:
                        extra_labels = json.loads(labels_str)
                    except json.JSONDecodeError:
                        pass

                intervals.append({
                    'start': start_minutes,
                    'end': end_minutes,
                    'alertname': alertname,
                    'namespace': namespace,
                    'severity': severity,
                    'silenced': silenced,
                    'extra_labels': extra_labels
                })

            except (KeyError, ValueError) as e:
                print(f"Warning: Skipping invalid row: {e}", file=sys.stderr)
                continue

    if not intervals:
        return alerts

    # Find maxEnd (matching simulate logic)
    # If Watchdog exists with end=0, use 0 as maxEnd (negative value format)
    # Otherwise, use the maximum end value (positive value format)
    watchdog_intervals = [i for i in intervals if i['alertname'] == 'Watchdog']
    if watchdog_intervals and watchdog_intervals[0]['end'] == 0:
        # Negative value format: Watchdog at 0,0 defines "now"
        max_end = 0
    else:
        # Positive value format: maximum end defines "now"
        max_end = max(interval['end'] for interval in intervals)

    # Calculate absStart = base_time - maxEnd minutes
    abs_start = base_time - timedelta(minutes=max_end)

    # Second pass: create alerts with absolute timestamps
    watchdog_max_ts = None
    for interval in intervals:
        alertname = interval['alertname']

        # Skip Watchdog from display but track its end time as the "now" reference
        if alertname == 'Watchdog':
            end_ts = int((abs_start + timedelta(minutes=interval['end'])).timestamp())
            if watchdog_max_ts is None or end_ts > watchdog_max_ts:
                watchdog_max_ts = end_ts
            continue

        # Build labels dictionary
        labels = {
            'alertname': alertname,
            'namespace': interval['namespace'],
            'severity': interval['severity'],
            'silenced': str(interval['silenced']).lower(),
        }
        labels.update(interval['extra_labels'])

        # Calculate absolute timestamps (in minutes, matching simulate logic)
        start_ts = int((abs_start + timedelta(minutes=interval['start'])).timestamp())
        end_ts = int((abs_start + timedelta(minutes=interval['end'])).timestamp())

        # Create unique key
        alert_key = f"{alertname}|{';'.join(f'{k}={v}' for k, v in sorted(labels.items()))}"

        # Create or get alert
        if alert_key not in alerts:
            alerts[alert_key] = Alert(alertname, labels)

        # Add the time segment
        alerts[alert_key].add_segment(start_ts, end_ts)

    return alerts, watchdog_max_ts


def parse_openmetrics_file(filepath: str) -> Tuple[Dict[str, Alert], Optional[int]]:
    """
    Parse OpenMetrics format file and extract alerts.

    Args:
        filepath: Path to the openmetrics file

    Returns:
        Tuple of (alerts dict, Watchdog max timestamp or None)
    """
    alerts = {}
    watchdog_max_ts = None

    # Regex to parse metric line
    # Format: ALERTS{label1="value1",label2="value2"} value timestamp
    metric_pattern = re.compile(
        r'ALERTS\{([^}]+)\}\s+[\d.]+\s+(\d+)'
    )

    with open(filepath, 'r') as f:
        for line in f:
            # Skip comments and empty lines
            if line.startswith('#') or not line.strip():
                continue

            match = metric_pattern.match(line.strip())
            if not match:
                continue

            labels_str, timestamp = match.groups()
            timestamp = int(timestamp)

            # Parse labels
            labels = {}
            label_pattern = re.compile(r'(\w+)="([^"]*)"')
            for label_match in label_pattern.finditer(labels_str):
                key, value = label_match.groups()
                labels[key] = value

            alertname = labels.get('alertname', 'Unknown')

            # Skip Watchdog from display but track its max timestamp as the "now" reference
            if alertname == 'Watchdog':
                if watchdog_max_ts is None or timestamp > watchdog_max_ts:
                    watchdog_max_ts = timestamp
                continue

            # Create or update alert
            alert_key = f"{alertname}|{';'.join(f'{k}={v}' for k, v in sorted(labels.items()))}"

            if alert_key not in alerts:
                alerts[alert_key] = Alert(alertname, labels)

            alerts[alert_key].add_timestamp(timestamp)

    return alerts, watchdog_max_ts


def detect_file_format(filepath: str) -> str:
    """
    Detect if the file is CSV or OpenMetrics format.

    Args:
        filepath: Path to the file

    Returns:
        'csv' or 'openmetrics'
    """
    with open(filepath, 'r') as f:
        first_line = f.readline().strip()

        # Check if it looks like CSV header
        if first_line.startswith('start,end,alertname'):
            return 'csv'

        # Otherwise assume OpenMetrics
        return 'openmetrics'


def plot_alert_timeline(alerts: Dict[str, Alert], output_file: str = None,
                        show_plot: bool = True, gap_tolerance: int = 600,
                        title: str = 'Alert Timeline Visualization',
                        use_relative_time: bool = False,
                        now_timestamp: Optional[int] = None):
    """
    Create a timeline visualization of alerts.

    Args:
        alerts: Dictionary of Alert objects
        output_file: Path to save the plot (optional)
        show_plot: Whether to display the plot interactively
        gap_tolerance: Maximum seconds between points to consider continuous
        use_relative_time: Display time as relative seconds instead of absolute dates
    """
    if not alerts:
        print("No alerts found to visualize")
        return

    # Sort alerts alphabetically by name and namespace
    sorted_alerts = sorted(alerts.values(),
                          key=lambda a: (a.alertname, a.namespace))

    # Determine severity colors
    severity_colors = {
        'critical': '#d73a4a',
        'warning': '#fb8c00',
        'info': '#0366d6',
        'none': '#6a737d'
    }

    # Create figure
    fig, ax = plt.subplots(figsize=(16, max(8, len(sorted_alerts) * 0.4)))

    # Find the minimum timestamp to use as baseline for relative time
    all_timestamps = []
    for alert in sorted_alerts:
        segments = alert.get_timeline_segments(tolerance_seconds=gap_tolerance)
        for start_ts, end_ts in segments:
            all_timestamps.extend([start_ts, end_ts])

    if not all_timestamps:
        print("No timeline data found")
        return

    min_timestamp = min(all_timestamps)
    max_timestamp = max(all_timestamps)

    # Use Watchdog's end timestamp as the "now" reference if available,
    # since Watchdog is filtered from display but defines the timeline anchor
    if now_timestamp is not None:
        max_timestamp = max(max_timestamp, now_timestamp)

    # Plot each alert
    for idx, alert in enumerate(sorted_alerts):
        # With inverted y-axis, idx=0 is at y=0 (top)
        y_position = idx
        segments = alert.get_timeline_segments(tolerance_seconds=gap_tolerance)

        color = severity_colors.get(alert.severity.lower(), severity_colors['none'])

        for seg_idx, (start_ts, end_ts) in enumerate(segments):
            if use_relative_time:
                # Calculate position relative to fixed 15-day range
                # Position 0 = now - 15 days
                # Position full_range_seconds = now
                # now = max_timestamp
                FULL_RANGE_DAYS = 15
                full_range_seconds = FULL_RANGE_DAYS * 86400
                range_start_ts = max_timestamp - full_range_seconds

                start_val = start_ts - range_start_ts
                end_val = end_ts - range_start_ts

                # Handle zero-width bars (give them minimum width of 1% of total range)
                if start_val == end_val:
                    end_val = start_val + max(1, full_range_seconds * 0.01)

                # Ensure bars never extend past "now" (the right edge)
                if end_val > full_range_seconds:
                    end_val = full_range_seconds

                width = end_val - start_val
            else:
                # Use absolute datetime
                start_date = datetime.fromtimestamp(start_ts)
                end_date = datetime.fromtimestamp(end_ts)
                start_val = start_date
                # Handle zero-width bars
                if start_ts == end_ts:
                    range_seconds = max_timestamp - min_timestamp
                    end_date = datetime.fromtimestamp(end_ts + max(1, range_seconds * 0.01))

                # Ensure bars never extend past max_timestamp
                max_date = datetime.fromtimestamp(max_timestamp)
                if end_date > max_date:
                    end_date = max_date

                width = end_date - start_date if not use_relative_time else end_val - start_val

            # Draw rectangle for this segment
            # Smaller bar height for better visibility
            bar_height = 0.2
            rect = Rectangle((start_val, y_position - bar_height/2), width, bar_height,
                           facecolor=color, edgecolor='black', linewidth=0.5,
                           alpha=0.7)
            ax.add_patch(rect)

            # Add alert name text inside every rectangle (including gaps)
            # Calculate text position (centered in the bar)
            text_x = start_val + width / 2
            text_y = y_position

            # Add text label with alert name
            ax.text(text_x, text_y, alert.alertname,
                   ha='center', va='center',
                   fontsize=8, color='white',
                   weight='bold',
                   bbox=dict(boxstyle='round,pad=0.3',
                            facecolor=color,
                            edgecolor='none',
                            alpha=0.8))

    # Configure axes
    # Invert y-axis so Alert0 appears at the top
    ax.set_ylim(len(sorted_alerts) - 0.5, -0.5)
    # Remove Y-axis labels - alert names are displayed inside rectangles
    ax.set_yticks([])
    ax.set_yticklabels([])

    # Format x-axis based on time mode
    if use_relative_time:
        # For CSV: show time relative to "now" going backwards
        # Always display the full 15-day range regardless of actual data span
        FULL_RANGE_DAYS = 15
        full_range_seconds = FULL_RANGE_DAYS * 86400

        # Set x-axis to always show full 15-day range
        ax.set_xlim(0, full_range_seconds)

        # Set specific tick positions: now-15d, now-7d, now-3d, now-1d, now
        # These are fixed markers at specific positions
        tick_positions = []
        tick_labels = []

        # Define the time points we want to show (in seconds from "now" going backwards)
        # Note: 86400 seconds = 1 day
        time_points = [
            (15 * 86400, 'now-15d'),  # 15 days ago
            (7 * 86400, 'now-7d'),     # 7 days ago
            (3 * 86400, 'now-3d'),     # 3 days ago
            (1 * 86400, 'now-1d'),     # 1 day ago
            (0, 'now')                 # current time
        ]

        for seconds_from_now, label in time_points:
            # Convert "seconds from now" to position on x-axis
            # x-axis position = full_range_seconds - seconds_from_now
            position = full_range_seconds - seconds_from_now
            tick_positions.append(position)
            tick_labels.append(label)

        # Set the ticks
        ax.set_xticks(tick_positions)
        ax.set_xticklabels(tick_labels)
        ax.set_xlabel('Time (relative to now)', fontsize=12)
    else:
        # Use absolute dates
        ax.set_xlim(datetime.fromtimestamp(min_timestamp),
                   datetime.fromtimestamp(max_timestamp))
        ax.xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m-%d %H:%M'))
        ax.xaxis.set_major_locator(mdates.AutoDateLocator())
        plt.setp(ax.xaxis.get_majorticklabels(), rotation=45, ha='right')
        ax.set_xlabel('Time', fontsize=12)

    # Labels and title
    # No Y-axis label needed - alert names are inside rectangles
    ax.set_title(title, fontsize=14, fontweight='bold')

    # Grid
    ax.grid(True, axis='x', alpha=0.3, linestyle='--')

    # Legend for severity
    from matplotlib.patches import Patch
    legend_elements = [
        Patch(facecolor=severity_colors['critical'], label='Critical', alpha=0.7),
        Patch(facecolor=severity_colors['warning'], label='Warning', alpha=0.7),
        Patch(facecolor=severity_colors['info'], label='Info', alpha=0.7),
    ]
    ax.legend(handles=legend_elements, loc='upper right')

    plt.tight_layout()

    # Save if output file specified
    if output_file:
        plt.savefig(output_file, dpi=300, bbox_inches='tight')
        print(f"Saved visualization to: {output_file}")

    # Show if requested
    if show_plot:
        plt.show()
    else:
        plt.close()


def main():
    parser = argparse.ArgumentParser(
        description='Visualize cluster-health-analyzer alert timelines',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Use default OpenMetrics file (cluster-health-analyzer-openmetrics.txt)
  %(prog)s
  %(prog)s -o alerts.png --no-show

  # CSV format (testdata scenario)
  %(prog)s testdata/simple_scenario.csv
  %(prog)s testdata/input.csv -o scenario.png --no-show

  # Adjust gap tolerance for OpenMetrics (default 600 seconds)
  %(prog)s -g 300

  # CSV with custom base time
  %(prog)s scenario.csv --base-time "2024-01-01 00:00:00"
        """
    )

    parser.add_argument('input_file',
                       nargs='?',
                       default='cluster-health-analyzer-openmetrics.txt',
                       help='Path to CSV or OpenMetrics format file (default: cluster-health-analyzer-openmetrics.txt)')
    parser.add_argument('-o', '--output',
                       help='Output file path for saved visualization (PNG, PDF, SVG)')
    parser.add_argument('--no-show',
                       action='store_true',
                       help='Do not display the plot interactively')
    parser.add_argument('-g', '--gap-tolerance',
                       type=int,
                       default=600,
                       help='Maximum gap in seconds to consider alerts continuous (default: 600, OpenMetrics only)')
    parser.add_argument('-v', '--verbose',
                       action='store_true',
                       help='Print detailed information about parsed alerts')
    parser.add_argument('--base-time',
                       help='Base time for CSV relative timestamps (format: "YYYY-MM-DD HH:MM:SS", default: now)')
    parser.add_argument('-f', '--format',
                       choices=['csv', 'openmetrics', 'auto'],
                       default='auto',
                       help='Input file format (default: auto-detect)')
    parser.add_argument('-t', '--title',
                       help='Custom title for the visualization')
    parser.add_argument('--relative-time',
                       action='store_true',
                       help='Display time as relative seconds (auto-enabled for CSV)')
    parser.add_argument('--absolute-time',
                       action='store_true',
                       help='Force absolute time display (overrides auto-detection)')

    args = parser.parse_args()

    # Parse base time if provided
    base_time = None
    if args.base_time:
        try:
            base_time = datetime.strptime(args.base_time, '%Y-%m-%d %H:%M:%S')
        except ValueError:
            print(f"Error: Invalid base-time format. Use 'YYYY-MM-DD HH:MM:SS'", file=sys.stderr)
            sys.exit(1)

    # Detect file format
    if args.format == 'auto':
        try:
            file_format = detect_file_format(args.input_file)
            print(f"Detected format: {file_format}")
        except Exception as e:
            print(f"Error detecting file format: {e}", file=sys.stderr)
            sys.exit(1)
    else:
        file_format = args.format

    # Parse the file
    print(f"Parsing {args.input_file}...")
    try:
        if file_format == 'csv':
            alerts, watchdog_max_ts = parse_csv_file(args.input_file, base_time=base_time)
        else:
            alerts, watchdog_max_ts = parse_openmetrics_file(args.input_file)
    except FileNotFoundError:
        print(f"Error: File not found: {args.input_file}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error parsing file: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        sys.exit(1)

    print(f"Found {len(alerts)} unique alerts")

    if args.verbose:
        for alert in sorted(alerts.values(), key=lambda a: a.alertname):
            segments = alert.get_timeline_segments(args.gap_tolerance)
            data_points = len(alert.timestamps) if alert.timestamps else len(alert.segments)
            print(f"  {alert.get_label()}: {data_points} data points, "
                  f"{len(segments)} segments")

    # Determine title
    title = args.title if args.title else 'Alert Timeline Visualization'

    # Determine time display mode
    if args.absolute_time:
        use_relative = False
    elif args.relative_time:
        use_relative = True
    else:
        # Always use relative time (now-15d, now-7d, now-3d, now-1d, now) for both CSV and OpenMetrics
        use_relative = True

    # Create visualization
    plot_alert_timeline(
        alerts,
        output_file=args.output,
        show_plot=not args.no_show,
        gap_tolerance=args.gap_tolerance,
        title=title,
        use_relative_time=use_relative,
        now_timestamp=watchdog_max_ts,
    )

    print("Done!")


if __name__ == '__main__':
    main()
