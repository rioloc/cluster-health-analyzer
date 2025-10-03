#!/bin/bash

# IMPORTANT! Make sure to export OPENAI_API_KEY when using gpt4.1 as judge llm

# Script to port-forward to lightspeed pod and test connectivity
set -e

# Check if lightspeed-eval tool is installed
if ! command -v lightspeed-eval &> /dev/null; then
    echo "Error: lightspeed-eval tool is not installed or not in PATH"
    echo "Please install the lightspeed-eval tool before running this script"
    exit 1
fi

NAMESPACE="openshift-lightspeed"
LABEL="app.kubernetes.io/name=lightspeed-service-api"
LOCAL_PORT="8080"
REMOTE_PORT="8443"  # Pod listens on 8443 for metrics

LS_EVAL_SYSTEM_CFG_PATH=$1
LS_EVAL_DATA_CFG_PATH=$2

# Function to cleanup port-forward on script exit
cleanup() {
    echo "Cleaning up..."
    if [ ! -z "$PORT_FORWARD_PID" ]; then
        echo "Killing port-forward (PID: $PORT_FORWARD_PID)"
        kill $PORT_FORWARD_PID 2>/dev/null || true
        wait $PORT_FORWARD_PID 2>/dev/null || true
    fi
}

# Set trap to cleanup on script exit (normal exit, interrupt, or termination)
trap cleanup EXIT INT TERM

# Get the running pod name
echo "Finding running pod with label $LABEL in namespace $NAMESPACE..."
POD_NAME=$(oc get pods -n $NAMESPACE -l $LABEL --field-selector=status.phase=Running -o jsonpath='{.items[0].metadata.name}')

if [ -z "$POD_NAME" ]; then
    echo "Error: No running pod found with label $LABEL"
    exit 1
fi

echo "Found pod: $POD_NAME"

# Start port-forward in background
echo "Starting port-forward from localhost:$LOCAL_PORT to $POD_NAME:$REMOTE_PORT..."
oc port-forward -n $NAMESPACE $POD_NAME $LOCAL_PORT:$REMOTE_PORT &
PORT_FORWARD_PID=$!

# Wait a moment for port-forward to establish
echo "Waiting for port-forward to establish..."
sleep 5

# Check if port-forward is still running
if ! kill -0 $PORT_FORWARD_PID 2>/dev/null; then
    echo "Error: Port-forward failed to start"
    exit 1
fi

API_KEY=$(oc whoami -t) lightspeed-eval --system-config ${LS_EVAL_SYSTEM_CFG_PATH} --eval-data ${LS_EVAL_DATA_CFG_PATH} --output eval_output
DETAILED_CSV=$(find eval_output -type f -name "*_detailed.csv" -printf '%T@ %p\n' | sort -n | tail -n 1 | cut -d' ' -f2-)

# Automatically printing detailed output

echo "######################################"

python -c '
import csv
import sys
import json

# The DictReader automatically uses the first row as headers
# and reads the rest of the rows as dictionaries.
reader = csv.DictReader(sys.stdin)
selected_data_list = []

for row in reader:
    # Each "row" is now an OrderedDict. We convert it to a regular dict.
    data = dict(row)

    # Create a new dictionary with only the desired keys
    selected_data = {
        "metric": data["metric_identifier"],
        "result": data["result"],
        "score": data["score"],
        "threshold": data["threshold"],
        "query": data["query"],
        "response": data["response"],
        "reason": data["reason"]
    }
    selected_data_list.append(selected_data)

print(json.dumps(selected_data_list, indent=4))

' < ${DETAILED_CSV}
