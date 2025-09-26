#!/bin/bash

# Configuration variables
GRAFANA_URL="${GRAFANA_URL:-http://shannon-grafana-1:3000}"
GRAFANA_USER="${GRAFANA_USER:-shannon}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-shannon}"
DATASOURCE_NAME="${DATASOURCE_NAME:-Prometheus}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://shannon-prometheus-1:9090}"
MAX_RETRIES=30
RETRY_INTERVAL=2

# Wait for Grafana to start
echo "Waiting for Grafana to start..."
for i in $(seq 1 $MAX_RETRIES); do
    if curl -s -o /dev/null -w "%{http_code}" "$GRAFANA_URL/api/health" | grep -q "200"; then
        echo "Grafana is ready"
        break
    fi
    echo "Waiting for Grafana to start... ($i/$MAX_RETRIES)"
    sleep $RETRY_INTERVAL
    if [ $i -eq $MAX_RETRIES ]; then
        echo "Error: Grafana startup timeout"
        exit 1
    fi
done

# Additional wait to ensure Grafana is fully ready
sleep 5

# Create or update Prometheus data source
echo "Configuring Prometheus data source..."
DATASOURCE_PAYLOAD=$(cat <<EOF
{
  "name": "$DATASOURCE_NAME",
  "type": "prometheus",
  "access": "proxy",
  "url": "$PROMETHEUS_URL",
  "isDefault": true,
  "editable": true
}
EOF
)

# Check if data source already exists
EXISTING_DS=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
    "$GRAFANA_URL/api/datasources/name/$DATASOURCE_NAME" 2>/dev/null)

if echo "$EXISTING_DS" | grep -q "\"id\""; then
    echo "Data source exists, updating..."
    DS_ID=$(echo "$EXISTING_DS" | grep -o '"id":[0-9]*' | cut -d: -f2)
    curl -s -X PUT \
        -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
        -H "Content-Type: application/json" \
        -d "$DATASOURCE_PAYLOAD" \
        "$GRAFANA_URL/api/datasources/$DS_ID"
else
    echo "Creating new data source..."
    curl -s -X POST \
        -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
        -H "Content-Type: application/json" \
        -d "$DATASOURCE_PAYLOAD" \
        "$GRAFANA_URL/api/datasources"
fi

echo ""

# Get data source UID
DS_RESPONSE=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
    "$GRAFANA_URL/api/datasources/name/$DATASOURCE_NAME")
DS_UID=$(echo "$DS_RESPONSE" | grep -o '"uid":"[^"]*' | cut -d'"' -f4)

if [ -z "$DS_UID" ]; then
    echo "Warning: Unable to get data source UID, using default"
    DS_UID="prometheus-uid"
fi

# Import Dashboard
import_dashboard() {
    local dashboard_file=$1
    local dashboard_name=$(basename "$dashboard_file" .json)

    echo "Importing Dashboard: $dashboard_name"

    if [ ! -f "$dashboard_file" ]; then
        echo "Error: Dashboard file not found: $dashboard_file"
        return 1
    fi

    # Read dashboard JSON
    DASHBOARD_CONTENT=$(cat "$dashboard_file")

    # Check if it's a Grafana.com dashboard (contains gnetId)
    if echo "$DASHBOARD_CONTENT" | jq -e '.gnetId' > /dev/null 2>&1; then
        echo "Detected Grafana.com dashboard, ID: $(echo "$DASHBOARD_CONTENT" | jq -r '.gnetId')"
        GNET_ID=$(echo "$DASHBOARD_CONTENT" | jq -r '.gnetId')

        # Step 1: Download dashboard JSON from Grafana.com
        echo "Downloading dashboard from Grafana.com..."
        DASHBOARD_JSON_URL="https://grafana.com/api/dashboards/$GNET_ID/revisions/latest/download"
        curl -s "$DASHBOARD_JSON_URL" > /tmp/dashboard.json

        if [ ! -s /tmp/dashboard.json ]; then
            echo "✗ Unable to download dashboard from Grafana.com"
            return 1
        fi

        # Build import request
        jq --arg uid "$DS_UID" '{
            dashboard: .,
            folderId: 0,
            overwrite: true,
            inputs: [{
                name: "DS_PROMETHEUS",
                type: "datasource",
                pluginId: "prometheus",
                value: $uid
            }]
        }' /tmp/dashboard.json > /tmp/import-request.json

        # 导入dashboard
        IMPORT_RESPONSE=$(curl -s -X POST \
            -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
            -H "Content-Type: application/json" \
            -d @/tmp/import-request.json \
            "$GRAFANA_URL/api/dashboards/import")

        if echo "$IMPORT_RESPONSE" | grep -q "\"imported\":true"; then
            echo "✓ Dashboard imported successfully"

            # Get imported dashboard UID
            IMPORTED_UID=$(echo "$IMPORT_RESPONSE" | jq -r '.uid')

            # Step 2: Fix datasource configuration
            echo "Fixing data source configuration..."
            CURRENT_DASHBOARD=$(curl -s -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
                "$GRAFANA_URL/api/dashboards/uid/$IMPORTED_UID")

            # Update datasource for all panels
            echo "$CURRENT_DASHBOARD" | jq --arg uid "$DS_UID" '
                .dashboard.templating.list = (.dashboard.templating.list // [] | map(
                    if .type == "datasource" and .query == "prometheus" then
                        .current = {"text": "Prometheus", "value": $uid}
                    else . end
                )) |
                .dashboard.panels = (.dashboard.panels // [] | map(
                    .datasource = {"type": "prometheus", "uid": $uid}
                )) |
                {dashboard: .dashboard, overwrite: true}
            ' > /tmp/fixed-dashboard.json

            # Update dashboard
            UPDATE_RESPONSE=$(curl -s -X POST \
                -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
                -H "Content-Type: application/json" \
                -d @/tmp/fixed-dashboard.json \
                "$GRAFANA_URL/api/dashboards/db")

            if echo "$UPDATE_RESPONSE" | grep -q "\"status\":\"success\""; then
                echo "✓ Data source configuration fixed successfully"
                return 0
            else
                echo "⚠ Dashboard imported but data source configuration fix failed"
                return 0
            fi
        else
            echo "✗ Dashboard import failed"
            echo "Error message: $IMPORT_RESPONSE"
            return 1
        fi
    else
        # Process local dashboard JSON
        DASHBOARD_JSON=$(echo "$DASHBOARD_CONTENT" | jq --arg uid "$DS_UID" '
            .dashboard = . |
            .dashboard.id = null |
            .dashboard.uid = null |
            .overwrite = true |
            .folderId = 0 |
            .inputs = [] |
            .dashboard.panels[]?.datasource.uid = $uid |
            .dashboard.templating?.list[]?.datasource.uid = $uid |
            .dashboard.annotations?.list[]?.datasource.uid = $uid
        ')

        # 导入dashboard
        IMPORT_RESPONSE=$(curl -s -X POST \
            -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
            -H "Content-Type: application/json" \
            -d "$DASHBOARD_JSON" \
            "$GRAFANA_URL/api/dashboards/import")

        if echo "$IMPORT_RESPONSE" | grep -q "\"imported\":true\|\"id\":[0-9]"; then
            echo "✓ Dashboard imported successfully: $dashboard_name"
            return 0
        else
            echo "✗ Dashboard import failed: $dashboard_name"
            echo "Error message: $IMPORT_RESPONSE"
            return 1
        fi
    fi
}

# Import all dashboard files
DASHBOARD_DIR="/dashboards"
if [ -d "$DASHBOARD_DIR" ]; then
    echo "Starting Dashboard import..."
    for dashboard in "$DASHBOARD_DIR"/*.json; do
        if [ -f "$dashboard" ]; then
            import_dashboard "$dashboard"
            echo ""
        fi
    done
    echo "All Dashboards imported successfully!"
else
    echo "Error: Dashboard directory not found: $DASHBOARD_DIR"
    exit 1
fi

echo "Grafana configuration complete!"
echo "Access URL: http://localhost:3000"
echo "Username: $GRAFANA_USER"
echo "Password: $GRAFANA_PASSWORD"