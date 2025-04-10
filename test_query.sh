#!/bin/bash

# Define the query parameters
PROM_QUERY='rate(container_cpu_usage_seconds_total{namespace="default"}[1m])'
TITLE="K8s CPU Usage in Default Namespace"
START=$(date -u -d '1 hour ago' +"%Y-%m-%dT%H:%M:%SZ")
END=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# The URL of your Go application (not Prometheus directly)
APP_URL="http://localhost:8080" # Change this if your app is running on a different port/host

# Create the JSON payload
DATA=$(jq -n \
    --arg query "$PROM_QUERY" \
    --arg title "$TITLE" \
    --arg start "$START" \
    --arg end "$END" \
    '{query: $query, title: $title, start: $start, end: $end}')

# Send the POST request using curl (or httpie if you prefer)
curl -X POST \
    -H "Content-Type: application/json" \
    -d "$DATA" \
    "$APP_URL"

# Alternative using httpie (if you have it installed)
# http POST "$APP_URL" \
#     Content-Type:application/json \
#     query="$PROM_QUERY" \
#     title="$TITLE" \
#     start="$START" \
#     end="$END"
