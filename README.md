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
- `rake deploy` — Deploy filter and configs
- `rake check` — Validate Logstash config
- `rake restart` — Restart Logstash
- `rake full_deploy` — Full pipeline
- `rake clean` — Remove temp files

## Deployment
Backups are created for existing files during deploy.
