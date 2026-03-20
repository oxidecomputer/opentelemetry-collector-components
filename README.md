# Oxide OpenTelemetry Collector Components

This repository provides OpenTelemetry Collector components for [Oxide](https://oxide.computer/).

## Receivers

- **[`oxide`](receiver/oxidemetricsreceiver/)** — collects metrics from the Oxide API
- **[`oxideauditlogs`](receiver/oxideauditlogsreceiver/)** — collects audit logs from the Oxide API

## Example Configuration

See [collector/config.example.yaml](collector/config.example.yaml) for a complete example that configures both receivers.

## Building an Otel Collector binary

This repository includes utilities to build an OpenTelemetry Collector binary that includes both receivers. For convenience, we also include the Otel components used in the [otelcol-contrib distribution](https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol-contrib) provided by the OpenTelemetry organization. To customize the set of Otel plugins used, update [collector/manifest.yaml](collector/manifest.yaml).

### Building the Collector

```bash
make build-collector
```

### Running the Collector

Create a `collector/config.yaml` file with your collector configuration, or copy from collector/config.example.yaml, then run:

```bash
./dist/otelcol-oxide --config collector/config.yaml
```

If using the default configuration, you can check metrics at `http://localhost:9091`. The collector will push audit logs to a local Loki instance, if available.

<details>
<summary>Verifying the Collector</summary>

<!--
Verification notes for LLMs:
- Our goal here is to verify the README. Follow the instructions here, and note if they're incomplete or incorrect.
- Ask the user whether they want to test the binary path, the docker compose path, or both.
- We need to rebuild all artifacts from source before testing: the otelcol-oxide binary if testing the binary path, the docker image if testing the docker compose path, etc. Otherwise, we may just verify that old artifacts work.
- Verification requires a real Oxide rack to query. If the OXIDE_HOST and OXIDE_TOKEN environment variables aren't already set, ask the user to provide them, or grant permission to grab them from ~/.config/oxide.
- To avoid hallucinating good behavior, we should show the user real output from the queries that we run against the OpenTelemetry Collector, Prometheus, etc. Before shutting down a given service, provide the user with a curl command to fetch real data, and prompt them to verify before tearing down the environment.
-->

The metrics receiver and audit logs receiver each collect data every 60 seconds by default (configurable via `collection_interval`). After waiting at least 60 seconds, verify that the Prometheus metrics endpoint is serving data:

```bash
curl -s http://localhost:9091/metrics
```

You should see Prometheus-formatted metrics like:

```
hardware_component:temperature{chassis_serial="BRM42220013",sensor="CPU",slot="14",...} 61.875
virtual_machine:check_total{instance_id="11d1e793-...",state="running",...} 19745
silo_utilization_cpu{silo_name="demo-1839bc12c06bd448",type="allocated",...} 32
virtual_disk:bytes_written_total{instance_id="11d1e793-...",...} 1.048576e+06
```

</details>

### Running the Collector with Docker Compose

We provide an example Dockerfile and Docker Compose manifest to run the Collector, along with a Prometheus instance to persist metrics. Note: the Docker Compose manifest doesn't mount your Oxide configuration file, so you can't authenticate using Oxide profiles. Instead, either set the `OXIDE_HOST` and `OXIDE_TOKEN` environment variables, or add authentication details to your OpenTelemetry configuration file.

```bash
docker compose -f example/docker-compose.yaml up
```

Once the example is running:

- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000
- Loki: http://localhost:3100
- Collector internal metrics: http://localhost:8888/metrics

<details>
<summary>Verifying Docker Compose</summary>

After waiting at least 60 seconds for the first collection cycle:

**Prometheus**: query for metrics, e.g. virtual machine vCPU usage:

```bash
curl -s 'http://localhost:9090/api/v1/query' \
  --data-urlencode 'query={__name__="virtual_machine:vcpu_usage_total"}' \
  | jq .
```

```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {
          "__name__": "virtual_machine:vcpu_usage_total",
          "instance_id": "11d1e793-6e66-4601-9754-a91e53f497f6",
          "state": "emulation",
          "vcpu_id": "0",
          ...
        },
        "value": [1740326494.082, "1370589028518"]
      },
      ...
    ]
  }
}
```

**Loki**: verify that audit logs are being ingested. Note: logs won't appear in Loki until the collector finishes its first full fetch of the audit log history, which may take several minutes if there are many entries in the lookback window.

```bash
curl -s -G http://localhost:3100/loki/api/v1/query_range \
  --data-urlencode 'query={service_name="oxide"}' \
  | jq .
```

Each log entry contains the full audit log JSON. The values are `[timestamp, body]` pairs:

```json
{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [
      {
        "stream": { "service_name": "oxide" },
        "values": [
          [
            "1740095568720139000",
            "{\"actor\":{\"kind\":\"unauthenticated\"},\"id\":\"c67a42df-3061-4dad-b2be-a3d57a6b99c7\",\"operation_id\":\"login_saml\",\"request_id\":\"3b53fe77-c1f0-4e96-865c-6ef7ccced0c0\",\"request_uri\":\"/login/demo-570ce8b7786fd50d/saml/keycloak\",\"result\":{\"http_status_code\":303,\"kind\":\"success\"},\"source_ip\":\"172.21.252.9\",\"time_completed\":\"2026-02-20T23:42:48.945813Z\",\"time_started\":\"2026-02-20T23:42:48.720139Z\"}"
          ]
        ]
      }
    ]
  }
}
```

</details>

## Example Dashboards

The example stack includes pre-provisioned Grafana dashboards that visualize metrics and audit logs provided by the receivers in this repo. These dashboards are built around the configuration used by the Docker Compose example. To use them for a separate deployment, you'll need to configure OpenTelemetry, Grafana, and Loki to match the dashboards:

- **Prometheus datasource**: set `timeInterval` to the scrape interval of your Prometheus target (60s for the example configuration).
- **Loki index labels**: The audit logs dashboard uses variables backed by Loki stream labels (`operation_id`, `actor_kind`, `actor_silo_user_id`). These require the `transform/promote-attrs` processor in the collector config and the `otlp_config` section in the Loki config. See [collector/config.example.yaml](collector/config.example.yaml) and [example/loki.yaml](example/loki.yaml).
- **Datasource UIDs**: The dashboards reference datasources by UID (`prometheus` and `loki`). The provisioned datasources must set matching UIDs.

Alternatively, you can adjust the dashboards to match your configuration.

### Installing dashboards in an existing Grafana instance

Download the JSON files from [example/grafana/dashboards](example/grafana/dashboards/) and import them via the Grafana UI (**Dashboards > New > Import**) or the [Grafana API](https://grafana.com/docs/grafana/latest/developer-resources/api-reference/http-api/dashboard/).

### Testing dashboards

The dashboards include end-to-end tests that verify every panel renders data. The tests use Playwright to load each dashboard in a headless browser and check that all panels have non-blank content. These tests don't verify correctness, but will catch dashboards that are fully broken. Note that tests require running the monitoring stack against a real Oxide rack, and don't run on CI. If you update a dashboard or build a new release of the components after an upstream change, run the tests manually to verify that the dashboards are still working.

To run the tests, start the example stack and wait ~3 minutes for data to accumulate:

```bash
# Start the stack
cd example
OXIDE_HOST=... OXIDE_TOKEN=... \
  docker compose up --build -d

# Wait for data (~3 minutes for rate() to have enough points)

# Run the tests
cd ..
npm install
npx playwright install chromium
npm run test:e2e
```

To run against a different Grafana instance:

```bash
GRAFANA_URL=https://grafana.example.com \
GRAFANA_USER=admin \
GRAFANA_PASSWORD=secret \
npm run test:e2e
```

## Contributing

See link:CONTRIBUTING.adoc[CONTRIBUTING.adoc] for development and release instructions.
