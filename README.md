# Logstash Custom Filter Deployment

## Overview
Custom Ruby filter and Logstash config deployment for firewall log ingestion.

## Structure
- `ruby/parse_filterlog.rb` — Ruby filter script
- `conf/*.conf` — Logstash pipeline configs
- `spec/` — RSpec tests
- `test_data/` — Sample logs

## Usage
- `rake test` — Run tests
- `rake lint` — Run RuboCop
- `rake format` — Auto-format Ruby
- `rake deploy` — Deploy filter and configs locally
- `rake check` — Validate Logstash config
- `rake restart` — Restart Logstash
- `rake full_deploy` — Full pipeline
- `rake clean` — Remove temp files

## Remote Deployment
Deploy to remote server via SSH:
```bash
REMOTE=1 rake deploy
REMOTE=1 rake restart
REMOTE=1 rake status
REMOTE=1 rake logs
```

## Deployment
Backups are created for existing files during deploy.

## Kibana Data View Creation

Create data views via Kibana Dev Tools Console for querying logs.

### Legacy Index Data View
For existing `opnsense-*` indices:

```json
POST kbn:/api/data_views/data_view
{
  "data_view": {
    "title": "opnsense-*",
    "name": "OPNsense Legacy",
    "timeFieldName": "@timestamp"
  }
}
```

### Data Stream Data View
For new `logs-opnsense.*` data streams:

```json
POST kbn:/api/data_views/data_view
{
  "data_view": {
    "title": "logs-opnsense.*",
    "name": "OPNsense Data Streams",
    "timeFieldName": "@timestamp"
  }
}
```

### Verify Data Views
List all data views:

```json
GET kbn:/api/data_views
```

### Access in Kibana
Navigate to **Discover** and select data view from dropdown.
