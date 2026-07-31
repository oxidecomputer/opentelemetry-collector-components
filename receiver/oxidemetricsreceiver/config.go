package oxidemetricsreceiver

import (
	"fmt"
	"regexp"
	"time"

	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`

	Host              string   `mapstructure:"host"`
	Token             string   `mapstructure:"token"`
	MetricPatterns    []string `mapstructure:"metric_patterns"`
	ScrapeConcurrency int      `mapstructure:"scrape_concurrency"`

	// QueryLookback configures the lookback interval of queries
	// sent to the Oxide API.
	QueryLookback string `mapstructure:"query_lookback"`

	// AddLabels configures the receiver to add human-readable labels to
	// metrics using the Oxide API.
	AddLabels bool `mapstructure:"add_labels"`

	// AddUtilizationMetrics configures the receiver to add silo utilization
	// metrics (cpu, memory, disk) with provisioned and allocated values.
	AddUtilizationMetrics bool `mapstructure:"add_utilization_metrics"`

	// InsecureSkipVerify configures the receiver to skip TLS certificate
	// verification when connecting to the Oxide API.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// SchemaRefreshInterval configures the interval at which the receiver refreshes the list of
	// available metrics from the Oxide API.
	SchemaRefreshInterval time.Duration `mapstructure:"schema_refresh_interval"`
}

func (cfg *Config) Validate() error {
	for _, pattern := range cfg.MetricPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("invalid metric pattern %s: %w", pattern, err)
		}
	}

	if cfg.SchemaRefreshInterval < 0 {
		return fmt.Errorf("invalid schema refresh interval %s", cfg.SchemaRefreshInterval)
	}

	return nil
}
