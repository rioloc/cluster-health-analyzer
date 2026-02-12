#!/bin/bash
#
# Example usage of the alert timeline visualizer
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "Alert Timeline Visualizer - Example Usage"
echo "=========================================="
echo ""

# Check if CSV testdata exists
CSV_FILE="${REPO_ROOT}/testdata/simple_scenario.csv"
if [ ! -f "$CSV_FILE" ]; then
    echo "Error: CSV file not found at: $CSV_FILE"
    exit 1
fi

echo "Using CSV file: $CSV_FILE"
echo ""

# Check for Python dependencies
if ! python3 -c "import matplotlib" 2>/dev/null; then
    echo "Installing required dependencies..."
    pip install -r "$SCRIPT_DIR/requirements.txt"
    echo ""
fi

# Example 1: CSV with verbose output
echo "Example 1: CSV scenario visualization (verbose mode)"
echo "-----------------------------------------------------"
python3 "$SCRIPT_DIR/render_alerts.py" "$CSV_FILE" -v --no-show -o /tmp/csv_scenario.png
echo ""

# Example 2: Complex CSV scenario
echo "Example 2: Complex CSV scenario"
echo "--------------------------------"
COMPLEX_CSV="${REPO_ROOT}/testdata/input.csv"
if [ -f "$COMPLEX_CSV" ]; then
    python3 "$SCRIPT_DIR/render_alerts.py" "$COMPLEX_CSV" --no-show -o /tmp/csv_complex.png -t "Complex Alert Scenario"
    echo "  Saved: /tmp/csv_complex.png"
fi
echo ""

# Example 3: CSV with custom base time
echo "Example 3: CSV with custom base time"
echo "-------------------------------------"
python3 "$SCRIPT_DIR/render_alerts.py" "$CSV_FILE" --base-time "2024-01-01 00:00:00" --no-show -o /tmp/csv_custom_time.png
echo "  Saved with custom base time: /tmp/csv_custom_time.png"
echo ""

# Example 4: Save to different formats
echo "Example 4: Saving to different formats"
echo "---------------------------------------"
python3 "$SCRIPT_DIR/render_alerts.py" "$CSV_FILE" --no-show -o /tmp/alerts.png
echo "  Saved PNG: /tmp/alerts.png"

python3 "$SCRIPT_DIR/render_alerts.py" "$CSV_FILE" --no-show -o /tmp/alerts.pdf
echo "  Saved PDF: /tmp/alerts.pdf"

python3 "$SCRIPT_DIR/render_alerts.py" "$CSV_FILE" --no-show -o /tmp/alerts.svg
echo "  Saved SVG: /tmp/alerts.svg"
echo ""

# Example 5: OpenMetrics format (if available)
METRICS_FILE="${REPO_ROOT}/cluster-health-analyzer-openmetrics.txt"
if [ -f "$METRICS_FILE" ]; then
    echo "Example 5: OpenMetrics format"
    echo "------------------------------"
    python3 "$SCRIPT_DIR/render_alerts.py" "$METRICS_FILE" --no-show -o /tmp/openmetrics.png -g 300
    echo "  Saved: /tmp/openmetrics.png"
    echo ""
fi

echo "=========================================="
echo "All examples completed successfully!"
echo ""
echo "Generated files in /tmp/:"
ls -lh /tmp/alerts* /tmp/csv_* /tmp/openmetrics* 2>/dev/null || true
echo ""
echo "To view interactively, run:"
echo "  python3 $SCRIPT_DIR/render_alerts.py $CSV_FILE"
echo ""
echo "Or with OpenMetrics format:"
if [ -f "$METRICS_FILE" ]; then
    echo "  python3 $SCRIPT_DIR/render_alerts.py $METRICS_FILE"
else
    echo "  Generate metrics first with: make simulate"
fi
