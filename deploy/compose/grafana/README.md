# Grafana + Prometheus Monitoring System

## Project Overview

This is a Docker Compose based monitoring solution that integrates Grafana, Prometheus, and Node Exporter for real-time monitoring of host performance and system metrics.

## System Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌────────────────┐
│                 │────▶│                  │────▶│                │
│   Node Exporter │     │   Prometheus     │     │    Grafana     │
│ (Collect Metrics)│     │  (Store Metrics) │     │ (Visualization)│
│                 │◀────│                  │◀────│                │
└─────────────────┘     └──────────────────┘     └────────────────┘
     :9100                    :9090                    :3000
```

## Component Description

**Note**: Grafana service uses port 3000. Shannon Dashboard has been moved to port 2111 to avoid conflicts.

### 1. **shannon-prometheus-1** (v2.39.0)
- **Function**: Time-series database responsible for collecting and storing metric data
- **Port**: 9090
- **Web UI**: http://localhost:9090
- **Data Collection Interval**: 15 seconds

### 2. **shannon-grafana-1** (latest)
- **Function**: Data visualization platform providing rich dashboards
- **Port**: 3000
- **Web UI**: http://localhost:3000
- **Default Credentials**: shannon / shannon
- **Features**: Auto-configured data sources and dashboards

### 3. **shannon-node-exporter-1** (latest)
- **Function**: Collect host hardware and operating system metrics
- **Port**: 9100
- **Metrics Endpoint**: http://localhost:9100/metrics
- **Monitoring Content**:
  - CPU usage (per core and overall)
  - Memory usage
  - Disk I/O and space utilization
  - Network traffic statistics
  - System load and uptime
  - Process and file descriptor statistics

### 4. **shannon-dashboard-importer** (alpine:latest)
- **Function**: Automatic dashboard and data source configuration
- **Type**: One-time execution container (exits after completion)
- **Tasks**:
  - Wait for Grafana to be fully ready
  - Configure Prometheus as default data source
  - Import all JSON dashboards from `/dashboards` directory
  - Fix data source references in imported dashboards
- **Restart Policy**: "no" (runs once and exits)

## Quick Start

### Prerequisites
- Docker and Docker Compose installed
- Ports 3000 (for Grafana), 9090, 9100 not in use
- Note: Shannon Dashboard now uses port 2111 instead of 3000

### Start Services

#### Quick Start Script
```bash
# Use the provided startup script
./start.sh
```

#### Manual Start
```bash
# Start all services
docker compose -f docker-compose-grafana-prometheus.yml up -d

# Check service status
docker ps | grep -E "shannon-prometheus-1|shannon-grafana-1|shannon-node-exporter-1"

# View service logs
docker logs shannon-grafana-1
docker logs shannon-prometheus-1
docker logs shannon-node-exporter-1
docker logs shannon-dashboard-importer
```

### Stop Services

```bash
# Stop and remove containers
docker compose -f docker-compose-grafana-prometheus.yml down

# Stop but retain data
docker compose -f docker-compose-grafana-prometheus.yml stop
```

## Directory Structure

```
/data/grafana/
├── README.md                                   # This document
├── docker-compose-grafana-prometheus.yml      # Docker Compose configuration
├── start.sh                                    # Service startup script
├── config/                                     # Configuration directory
│   ├── grafana.ini                            # Grafana configuration file
│   ├── prometheus.yml                         # Prometheus configuration file
│   └── provisioning/                          # Grafana provisioning directory
│       ├── datasources/
│       │   └── prometheus.yml                 # Data source configuration
│       └── dashboards/
│           └── node-exporter-1860.json        # Node Exporter dashboard (ID: 1860)
├── scripts/                                    # Script directory
│   └── import-dashboards.sh                   # Dashboard import script
└── data/                                      # Data storage directory
    ├── prometheus-data/                       # Prometheus data storage
    └── grafana-data/                          # Grafana data storage
```

## Dashboard Import System

### Overview
This project uses an optimized dashboard loading logic that has been changed from the previous provisioning static configuration to dynamic import via API after service startup.

### How It Works

1. **Grafana Service Startup**: The `shannon-grafana-1` container starts normally without mounting provisioning configuration
2. **Automatic Importer**: The `shannon-dashboard-importer` container runs automatically after Grafana starts
3. **Import Process**:
   - Wait for Grafana service to be fully ready
   - Automatically configure Prometheus data source
   - Import all JSON dashboard files from `/dashboards` directory
   - Container exits automatically after import completes

### Dashboard Management

#### Add New Dashboard
1. Place the new dashboard JSON file in the `config/provisioning/dashboards/` directory
2. Re-run the importer:
```bash
docker-compose -f docker-compose-grafana-prometheus.yml up shannon-dashboard-importer
```

#### Manual Dashboard Import
If you need to manually import dashboards:
```bash
docker exec -it shannon-dashboard-importer /import-dashboards.sh
```

### Configuration Parameters

The importer supports the following environment variables (set in docker-compose.yml):
- `GRAFANA_URL`: Grafana service address (default: http://shannon-grafana-1:3000)
- `GRAFANA_USER`: Grafana admin username (default: shannon)
- `GRAFANA_PASSWORD`: Grafana admin password (default: shannon)
- `DATASOURCE_NAME`: Prometheus data source name (default: Prometheus)
- `PROMETHEUS_URL`: Prometheus service address (default: http://shannon-prometheus-1:9090)

### Advantages

1. **Flexibility**: Can dynamically add/update dashboards at runtime
2. **Maintainability**: Dashboard files are separated from configuration, facilitating version control
3. **Automation**: All configurations are completed automatically after service startup
4. **Idempotency**: Repeated runs do not produce side effects, existing configurations are updated

## Automatic Configuration

The following configurations are automatically completed at system startup:

1. **Data Source Configuration**: Automatically add Prometheus as the default data source
2. **Dashboard Import**: Automatically import Node Exporter Full dashboard (Dashboard ID: 1860)
3. **Target Monitoring**: Prometheus automatically scrapes all configured targets

## Access and Usage

### 1. Access Grafana

1. Open http://localhost:3000 in browser (Grafana)
   Note: Shannon Dashboard is now accessible at http://localhost:2111
2. Login with default credentials: shannon / shannon
3. You may be prompted to change password on first login

### 2. View Dashboards

After login, the "Node Exporter Full" dashboard is automatically visible, including:
- System overview (CPU, memory, disk, network)
- Detailed performance charts
- Real-time data updates (15-second refresh)

### 3. Access Prometheus

- Web UI: http://localhost:9090
- View monitoring targets: http://localhost:9090/targets
- Execute PromQL queries: http://localhost:9090/graph

## Common Operations

### Check Monitoring Target Status

```bash
# Via API
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool

# View specific metrics
curl -s http://localhost:9100/metrics | grep node_cpu
```

### Restart Services (After Configuration Changes)

```bash
docker compose -f docker-compose-grafana-prometheus.yml restart

# Or completely rebuild
docker compose -f docker-compose-grafana-prometheus.yml down && docker compose -f docker-compose-grafana-prometheus.yml up -d
```

### Check Data Usage

```bash
# Prometheus data
du -sh data/prometheus-data/

# Grafana data
du -sh data/grafana-data/
```

## Troubleshooting

### 1. Service Cannot Start

```bash
# Check port usage
netstat -tunlp | grep -E "3000|9090|9100|2111"
# Port 3000: Grafana
# Port 2111: Shannon Dashboard
# Port 9090: Prometheus
# Port 9100: Node Exporter

# View detailed logs
docker compose -f docker-compose-grafana-prometheus.yml logs -f
```

### 2. Cannot Collect Metrics

```bash
# Check Node Exporter
curl http://localhost:9100/metrics

# Check Prometheus target status
curl http://localhost:9090/api/v1/targets
```

### 3. Grafana Cannot Display Data

- Check data source configuration: Settings → Data Sources
- Confirm Prometheus address: http://shannon-prometheus-1:9090
- Test data source connection

### 4. Dashboard Not Imported

- Check importer logs: `docker logs shannon-dashboard-importer`
- Confirm Grafana service is running: `docker ps | grep grafana`
- Verify dashboard file format is correct

### 5. Data Source Connection Failed

- Confirm Prometheus service is running normally
- Check network connectivity
- Verify environment variable configuration is correct

## Performance Optimization Suggestions

1. **Data Retention**: Default 15 days retention, adjustable in config/prometheus.yml
2. **Collection Interval**: Currently 15 seconds, adjustable as needed
3. **Disk Space**: Regularly check data/prometheus-data directory size
4. **Container Resources**: Resource limits can be added in docker-compose-grafana-prometheus.yml

## Extending Monitoring

### Add New Monitoring Targets

Edit `config/prometheus.yml`, add under `scrape_configs`:

```yaml
- job_name: 'my-app'
  static_configs:
    - targets: ['my-app:port']
```

Then restart Prometheus:

```bash
docker compose -f docker-compose-grafana-prometheus.yml restart shannon-prometheus-1
```

### Add New Exporters

1. Add new service in docker-compose-grafana-prometheus.yml
2. Configure scraping in config/prometheus.yml
3. Import corresponding dashboard in Grafana

## Security Recommendations

1. **Change Default Password**: Change admin password immediately after first login
2. **Restrict Access**: Consider using firewall rules to limit port access
3. **HTTPS Configuration**: SSL/TLS configuration recommended for production
4. **Data Backup**: Regularly backup data/grafana-data and data/prometheus-data

## Maintenance Tasks

- **Weekly**: Check disk usage
- **Monthly**: Review and optimize dashboard performance
- **Quarterly**: Update container image versions
- **As Needed**: Clean old data or adjust retention policies

## Related Resources

- [Prometheus Official Documentation](https://prometheus.io/docs/)
- [Grafana Official Documentation](https://grafana.com/docs/)
- [Node Exporter Documentation](https://github.com/prometheus/node_exporter)
- [PromQL Query Language](https://prometheus.io/docs/prometheus/latest/querying/basics/)

## Issue Feedback

If you encounter issues, please check:
1. Docker service is running normally
2. Ports are mapped correctly
3. Container logs for error messages
4. Network connections are normal

---

