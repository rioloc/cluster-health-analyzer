#!/bin/bash

SCENARIO=$1 make simulate
./scripts/visualize/render_alerts.py -o scenario.png && code scenario.png
