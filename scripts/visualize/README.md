# Alert Timeline Visualizer

A graphical visualization tool for rendering cluster-health-analyzer alert data as timeline charts.

## Overview

This tool parses CSV scenario files or OpenMetrics format files and creates visual timelines showing when alerts were firing. Each unique alert is displayed on its own horizontal line, with bars indicating firing periods and gaps showing when the alert was not active.

**Supported Input Formats:**
- **CSV**: Testdata scenario format (`testdata/*.csv`)
- **OpenMetrics**: ALERTS metric output from cluster-health-analyzer

## Features

- **Timeline Visualization**: Displays alerts as compact horizontal bars over time
- **Inline Labels**: Alert names displayed inside the bars for clean visualization
- **Gap Detection**: Automatically shows gaps when alerts stop and restart - same alert with multiple time periods appears on one line with gaps
- **Alphabetical Ordering**: Alerts ordered alphabetically from top to bottom (Alert0, Alert1, Alert2, etc.)
- **Fixed Time Range**: X-axis always displays full 15-day range for consistent visualization across scenarios
- **Severity Color Coding**: Different colors for critical, warning, and info severities
- **Unique Alert Tracking**: Each combination of alert name and labels gets its own line
- **Flexible Output**: Save to file (PNG, PDF, SVG) or display interactively
- **Smart Filtering**: Automatically filters out Watchdog marker alerts from CSV files
- **Fixed Time Markers**: X-axis shows fixed labels: `now-15d`, `now-7d`, `now-3d`, `now-1d`, `now`

## Installation

Install the required dependencies:

```bash
pip install -r requirements.txt
```

Or install manually:

```bash
pip install matplotlib numpy
```

## Usage

### OpenMetrics Format (Default)

Visualize cluster-health-analyzer output - the script defaults to reading `cluster-health-analyzer-openmetrics.txt`:

```bash
# Display interactively (uses default file)
./render_alerts.py

# Save to file (uses default file)
./render_alerts.py -o alerts.png --no-show

# Specify a different OpenMetrics file
./render_alerts.py path/to/custom-openmetrics.txt
```

### CSV Format (Testdata Scenarios)

Visualize alert scenarios from CSV files:

```bash
# Display interactively
./render_alerts.py ../../testdata/simple_scenario.csv

# Save to file
./render_alerts.py ../../testdata/input.csv -o scenario.png --no-show

# With custom base time for relative timestamps
./render_alerts.py ../../testdata/input.csv --base-time "2024-01-01 00:00:00"

# Verbose output
./render_alerts.py ../../testdata/input.csv -v
```

### Adjust Gap Tolerance (OpenMetrics Only)

For OpenMetrics format, the tool considers timestamps more than 600 seconds (10 minutes) apart as separate firing periods. You can adjust this:

```bash
# Consider gaps larger than 5 minutes (300 seconds)
./render_alerts.py ../../cluster-health-analyzer-openmetrics.txt -g 300

# More tolerant - 20 minutes
./render_alerts.py ../../cluster-health-analyzer-openmetrics.txt -g 1200
```

Note: Gap tolerance doesn't apply to CSV format since gaps are explicit in the data.

### Custom Title

Add a custom title to the visualization:

```bash
./render_alerts.py scenario.csv -t "Production Incident Timeline" -o incident.png
```

### Verbose Output

Get detailed information about parsed alerts:

```bash
./render_alerts.py input_file.csv -v
```

## Command Line Options

```
positional arguments:
  input_file              Path to CSV or OpenMetrics format file (auto-detected)

optional arguments:
  -h, --help              Show help message
  -o, --output FILE       Output file path for saved visualization (PNG, PDF, SVG)
  --no-show               Do not display the plot interactively
  -g, --gap-tolerance N   Maximum gap in seconds to consider alerts continuous
                          (default: 600, applies to OpenMetrics only)
  -v, --verbose           Print detailed information about parsed alerts
  -f, --format {csv,openmetrics,auto}
                          Input file format (default: auto-detect)
  -t, --title TITLE       Custom title for the visualization
  --base-time "YYYY-MM-DD HH:MM:SS"
                          Base time for CSV relative timestamps (default: now)
  --relative-time         Display time as relative seconds (auto-enabled for CSV)
  --absolute-time         Force absolute time display (overrides auto-detection)
```

## Examples

### Example 1: Quick View

```bash
cd scripts/visualize
./render_alerts.py ../../cluster-health-analyzer-openmetrics.txt
```

This will open an interactive matplotlib window showing the alert timeline.

### Example 2: Generate Report

```bash
./render_alerts.py ../../cluster-health-analyzer-openmetrics.txt \
  -o alert_report.png \
  --no-show \
  -v
```

This will:
- Parse the metrics file
- Print verbose information about found alerts
- Save a high-resolution PNG image
- Skip the interactive display

### Example 3: Different Formats

```bash
# Save as PDF for documentation
./render_alerts.py input.txt -o alerts.pdf --no-show

# Save as SVG for editing in Inkscape/Illustrator
./render_alerts.py input.txt -o alerts.svg --no-show
```

## Understanding the Visualization

### Timeline Bars

- Each compact horizontal bar represents a period when an alert was firing
- Alert names are displayed inside each bar with color-coded backgrounds
- Gaps in the bars indicate the alert stopped and later resumed
- When an alert has gaps, the alert name appears in every rectangle
- The x-axis shows time with fixed labels (now-15d, now-7d, now-3d, now-1d, now)
- Alerts are ordered alphabetically from top to bottom

### Color Coding

- **Red**: Critical severity alerts
- **Orange**: Warning severity alerts
- **Blue**: Info severity alerts
- **Gray**: Unknown/no severity

### Alert Identification

Each unique alert is identified by:
- Alert name (e.g., "AlertWGaps1_v2_1_Alert0")
- All labels (namespace, severity, alertstate, silenced, etc.)

So the same alert name with different labels will appear on separate lines.

## Input File Formats

### CSV Format (Testdata Scenarios)

The tool supports CSV files in the testdata scenario format:

```csv
start,end,alertname,namespace,severity,silenced,labels
0,4000,MyAlert,openshift-monitoring,warning,false,
720,17160,AnotherAlert,openshift-dns,critical,false,"{""node"": ""worker-1""}"
```

Columns:
- `start`: Start time in **minutes** (relative to scenario timeline)
- `end`: End time in **minutes** (relative to scenario timeline)
- `alertname`: Name of the alert
- `namespace`: Kubernetes namespace
- `severity`: Severity level (critical, warning, info, none)
- `silenced`: Whether the alert is silenced (true/false)
- `labels`: Additional labels as JSON object (optional)

**Time Calculation (matching simulate logic):**
1. Find `maxEnd` = maximum end value across all alerts
2. Calculate `absStart = now - maxEnd minutes`
3. Each alert runs from `absStart + start` to `absStart + end`
4. **Watchdog alert** (start=0, end=0) is filtered out from visualization

**Time Display:**
- By default, CSV files display time relative to "now" going backwards
- X-axis shows fixed labels: `now-15d`, `now-7d`, `now-3d`, `now-1d`, `now`
- Use `--absolute-time` to show actual timestamps instead
- Use `--base-time` to set a custom "now" time

### OpenMetrics Format

The tool expects OpenMetrics format with ALERTS metrics:

```
# HELP ALERTS Alert status
# TYPE ALERTS gauge
ALERTS{silenced="false",alertname="MyAlert",namespace="default",severity="warning"} 1.0 1769703678
ALERTS{silenced="false",alertname="MyAlert",namespace="default",severity="warning"} 1.0 1769703978
...
```

Each line contains:
- Metric name: `ALERTS`
- Labels in curly braces
- Value (typically 1.0 for firing)
- Unix timestamp

**Time Display:**
- By default, OpenMetrics files display absolute timestamps
- Use `--relative-time` to show time relative to first data point

## Troubleshooting

### No alerts displayed

Check that:
- The input file contains ALERTS metrics (not just comments)
- The file format matches OpenMetrics format
- Use `-v` to see what was parsed

### Visualization is too crowded

- Save to a larger file: use higher DPI by editing the script
- Filter alerts before visualization
- Adjust the figure size in the script

### Import errors

Make sure matplotlib and numpy are installed:
```bash
pip install --upgrade matplotlib numpy
```

## Integration with cluster-health-analyzer

This tool is designed to work with output from the `simulate` command:

```bash
# Generate metrics
make simulate

# Visualize the results
cd scripts/visualize
./render_alerts.py ../../cluster-health-analyzer-openmetrics.txt -o results.png
```

## Contributing

Feel free to enhance the visualization with:
- Additional filtering options
- Interactive features (zoom, pan)
- Export to different formats
- Statistical summaries
- Alert correlation analysis
