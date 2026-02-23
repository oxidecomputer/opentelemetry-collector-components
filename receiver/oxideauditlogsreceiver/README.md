# Oxide Audit Logs Receiver

The `oxideauditlogs` receiver collects audit logs from the [Oxide API](https://docs.oxide.computer/api) and converts them to OpenTelemetry logs.

## Configuration

All configuration parameters are optional. If `host` and `token` are not provided, the receiver will attempt to read them from the environment using the defaults [defined in the Oxide SDK](https://github.com/oxidecomputer/oxide.go#authentication).

### Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `host` | string | (from environment) | Oxide API host URL |
| `token` | string | (from environment) | Authentication token for the Oxide API |
| `initial_lookback` | duration | `1h` | How far back to query on the first scrape |
| `cursor_path` | string | (none) | Optional file path for persisting the pagination cursor across restarts |
| `page_size` | int | `500` | Number of audit log entries fetched per API request |
| `insecure_skip_verify` | bool | `false` | Skip TLS certificate verification |
| `collection_interval` | duration | `1m` | Interval between scrapes |

### Example

```yaml
receivers:
  oxideauditlogs:
    initial_lookback: 24h
    collection_interval: 60s
```

### Cold Start Performance

The initial scrape can be slow if `initial_lookback` is long and there are many audit log entries to fetch. To mitigate cold start times, use a shorter `initial_lookback`, or enable the persistent cursor using `cursor_path`.

### Internal Metrics

The receiver exposes metrics about its own operation via the collector's telemetry endpoint:

| Metric | Type | Description |
|--------|------|-------------|
| `oxide_audit_logs_receiver.scrape.count` | counter | Number of scrapes, labeled by `status` (success/failure) |
| `oxide_audit_logs_receiver.scrape.duration` | gauge | Duration of the most recent scrape (seconds) |
| `oxide_audit_logs_receiver.api_request.duration` | gauge | Duration of individual API page requests (seconds) |
