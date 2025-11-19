.PHONY: build install-builder clean test

# OpenTelemetry Collector Builder version
BUILDER_VERSION ?= 0.140.0

test:
	go test ./...

install-builder:
	@echo "Installing OpenTelemetry Collector Builder v$(BUILDER_VERSION)..."
	go install go.opentelemetry.io/collector/cmd/builder@v$(BUILDER_VERSION)

build-collector: install-builder
	@echo "Building custom OTEL collector with oxidereceiver..."
	builder --config collector/manifest.yaml

clean:
	@echo "Cleaning build artifacts..."
	rm -rf dist
