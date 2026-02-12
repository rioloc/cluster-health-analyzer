# Quick Start Guide

## 1. Install Dependencies

```bash
pip install -r requirements.txt
```

## 2. Run Visualization

### Quick Start (Default OpenMetrics)
```bash
# Uses cluster-health-analyzer-openmetrics.txt by default
./render_alerts.py

# Save to file
./render_alerts.py -o alerts.png --no-show
```

### CSV Format (Testdata Scenarios)
```bash
# Interactive view
./render_alerts.py ../../testdata/simple_scenario.csv

# Save to file
./render_alerts.py ../../testdata/input.csv -o scenario.png --no-show
```

## 3. Common Options

| Option | Description | Example |
|--------|-------------|---------|
| `-o FILE` | Save to file | `-o alerts.png` |
| `--no-show` | Don't display interactively | `--no-show` |
| `-g N` | Gap tolerance in seconds | `-g 300` |
| `-v` | Verbose output | `-v` |

## 4. Output Formats

- PNG: `-o alerts.png`
- PDF: `-o alerts.pdf`
- SVG: `-o alerts.svg`

## 5. Understanding the Visualization

- **X-axis**: Time relative to "now" (CSV) or absolute time (OpenMetrics)
  - CSV format: Fixed 15-day range with labels at `now-15d`, `now-7d`, `now-3d`, `now-1d`, `now`
  - OpenMetrics format: Actual dates/times
- **Alert Ordering**: Alerts are ordered alphabetically from top to bottom
  - Alert0 appears at the top, Alert1 below it, Alert2, etc.
- **Bars**: Compact horizontal rectangles showing periods when alerts were firing
  - Alert names are displayed inside each rectangle
- **Gaps**: Periods when alerts were not active - same alert can have multiple bars with gaps between them
  - Labels appear in all rectangles, even when the same alert has gaps

### Colors
- 🔴 Red: Critical
- 🟠 Orange: Warning
- 🔵 Blue: Info

### Notes
- **Watchdog alerts** are automatically filtered out from CSV visualizations
- Time values in CSV are in **minutes**, not seconds

## 6. Examples

Run the example script:
```bash
./example.sh
```

Or check the full [README.md](README.md) for detailed documentation.
