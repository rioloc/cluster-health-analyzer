#!/bin/bash

cat cluster-health-analyzer-openmetrics.txt | grep group_id | awk -F"group_id" '{ print $2 }' | cut -d"," -f1 | cut -d "\"" -f2 | sort | uniq | wc -l
