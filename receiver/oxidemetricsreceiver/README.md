# Oxide metrics receiver

The `oxide` receiver collects metrics from the [Oxide API](https://docs.oxide.computer/api) and converts them to OpenTelemetry metrics.

## Configuration

All configuration parameters are optional. If `host` and `token` are not provided, the receiver will attempt to read them from the environment using the defaults [defined in the Oxide SDK](https://github.com/oxidecomputer/oxide.go#authentication).

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `host` | string | (from environment) | The Oxide API host URL. Read from the environment by default. |
| `token` | string | (from environment) | The Oxide API token. Read from the environment by default. |
| `metric_patterns` | []string | `[".*"]` | The set of metric names to collect. |
| `scrape_concurrency` | int | `8` | The maximum number of concurrent OxQL queries to run. |
| `query_mode` | string | `last` | The OxQL query config pattern. If `last`, we use `\| last 1` to expose the most recent value of each series, which is appropriate for use with `prometheusexporter`, which only considers the most recent value for each series. If `window`, we query all metrics within the configured window, such that we retain the full fidelity of the OxQL metrics. This also allows us to query OxQL less often without losing resolution, and reduce load on OxQL. |
| `query_lookback` | duration | `5m` | The lookback interval of queries sent to the Oxide API. Only used for the `last` query mode. |
| `query_offset` | duration | `5s` | The offset applied to the end of the query window, only used for the `window` query mode. Because samples can arrive in oximeter later than their recorded timestamp, we include an offset so that late-arriving samples aren't dropped. |
| `max_window_size` | duration | `2x collection_interval` | The longest allowed query window, only used for the `window` query mode. If the query window exceeds `max_window_size`, e.g. to catch up after a restart or failed collection, we left-truncate to avoid overwhelming oximeter. |
| `add_labels` | bool | `false` | Add human-readable labels (silo and project names) to metrics using the Oxide API. |
| `add_utilization_metrics` | bool | `false` | Add silo utilization metrics (cpu, memory, disk) with provisioned and allocated values. |
| `insecure_skip_verify` | bool | `false` | Skip TLS certificate verification when connecting to the Oxide API. |
| `schema_refresh_interval` | duration | `5m` | The interval at which the receiver refreshes the list of available metrics from the Oxide API. |
| `collection_interval` | duration | `1m` | Interval between scrapes. |
| `initial_delay` | duration | `1s` | Delay before the first scrape. |
| `timeout` | duration | `0` | Timeout for a single scrape. `0` means no timeout. |

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

### Internal metrics

The receiver exposes metrics about its own operation via the collector's telemetry endpoint:

| Metric | Type | Description |
|--------|------|-------------|
| `oxide_receiver.scrape.count` | counter | Number of scrapes, labeled by `status` (success/failure) |
| `oxide_receiver.scrape.duration` | gauge | Duration of the most recent scrape (seconds) |
| `oxide_receiver.scrape.window_truncate_duration` | counter | Duration the collection window was truncated due to exceeding `max_window_size` (seconds) |
| `oxide_receiver.api_request.duration` | gauge | Duration of individual API requests (seconds) |
