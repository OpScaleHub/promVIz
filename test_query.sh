#!/bin/bash

# Define the query parameters
PROM_QUERY='rate(container_cpu_usage_seconds_total{namespace="default"}[1m])'
TITLE="K8s CPU Usage in Default Namespace"

# Send the POST request using httpie
http POST http://localhost:8080/ \
    Content-Type:application/json \
    query="$PROM_QUERY" \
    title="$TITLE"
