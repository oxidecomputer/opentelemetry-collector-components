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

## Development

### Running Tests

```bash
make test
```

## Releasing

To create a new release, first bump the versions in `collector/manifest.yaml`:

- `dist.version` — the collector distribution version
- The receiver gomod version (`github.com/oxidecomputer/opentelemetry-collector-components`)

Then push a git tag matching `v*` (e.g. `v0.2.0`). This creates a new GitHub release, and publishes updated collector binaries and the Docker image.

```bash
git tag v0.2.0
git push origin v0.2.0
```
