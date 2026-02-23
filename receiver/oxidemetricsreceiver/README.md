# Oxide Metrics Receiver

The `oxide` receiver collects metrics from the [Oxide API](https://docs.oxide.computer/api) and converts them to OpenTelemetry metrics.

## Configuration

All configuration parameters are optional. If `host` and `token` are not provided, the receiver will attempt to read them from the environment using the defaults [defined in the Oxide SDK](https://github.com/oxidecomputer/oxide.go#authentication).

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `host` | string | (from environment) | Oxide API host URL |
| `token` | string | (from environment) | Authentication token for the Oxide API |
| `metric_patterns` | []string | `[".*"]` | Regex patterns to filter which metrics to collect |
| `scrape_concurrency` | int | `16` | Number of concurrent API requests when scraping |
| `query_lookback` | string | `"5m"` | Lookback interval for queries sent to the Oxide API |
| `add_labels` | bool | `false` | Add human-readable labels (silo and project names) to metrics |
| `add_utilization_metrics` | bool | `false` | Add silo utilization metrics (cpu, memory, disk) |
| `insecure_skip_verify` | bool | `false` | Skip TLS certificate verification |
| `collection_interval` | duration | `1m` | Interval between scrapes |

### Example

```yaml
receivers:
  oxide:
    metric_patterns:
    - virtual_machine:.*
    - virtual_disk:.*
    - hardware_component:.*
    add_labels: true
    add_utilization_metrics: true
    collection_interval: 60s
```

### Internal Metrics

The receiver exposes metrics about its own operation via the collector's telemetry endpoint:

| Metric | Type | Description |
|--------|------|-------------|
| `oxide_receiver.scrape.count` | counter | Number of scrapes, labeled by `status` (success/failure) |
| `oxide_receiver.scrape.duration` | gauge | Duration of the most recent scrape (seconds) |
| `oxide_receiver.api_request.duration` | gauge | Duration of individual API requests (seconds) |
| `oxide_receiver.metric.parse_errors` | counter | Number of metric parse errors, labeled by `metric_name` |
