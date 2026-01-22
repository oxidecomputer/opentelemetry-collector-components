.PHONY: build clean test lint fmt build-collector

test:
	go test -v ./...

build-collector:
	go tool builder --config collector/manifest.yaml

clean:
	@echo "Cleaning build artifacts..."
	rm -rf dist

lint:
	go tool golangci-lint run

fmt:
	go tool golangci-lint fmt
