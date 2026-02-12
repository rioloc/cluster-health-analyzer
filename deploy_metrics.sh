#!/bin/bash

for i in $(find ./testdata -name "*.csv"); do
  echo "Removing previous openmetrics file"
  rm cluster-health-analyzer-openmetrics.txt

  echo "Cleaning data dir"
  rm -rf data
  
  echo "Deploying ${i}"

  SCENARIO=${i} make simulate && \
    promtool tsdb create-blocks-from openmetrics cluster-health-analyzer-openmetrics.txt && \
    for d in data/*; do echo $d && oc cp $d openshift-monitoring/prometheus-k8s-0:/prometheus -c prometheus; done;
done;
