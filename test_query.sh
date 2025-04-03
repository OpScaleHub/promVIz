#!/bin/bash

# Define the query parameters
PROM_QUERY='rate(container_cpu_usage_seconds_total{namespace="default"}[1m])'
TITLE="K8s CPU Usage in Default Namespace"
START=$(date -u -d '1 hour ago' +"%Y-%m-%dT%H:%M:%SZ")
END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
STEP="60s"

# Send the POST request using httpie
http POST http://localhost:8080/ \
    Content-Type:application/json \
    query="$PROM_QUERY" \
    title="$TITLE" \
    start="$START" \
    end="$END" \
    step="$STEP"
