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

	// QueryMode configures the OxQL query config pattern. If `last`, we use `| last 1` to expose
	// the most recent value of each series. This is the default behavior, and is appropriate for
	// use with `prometheusexporter`, which only considers the most recent value for each series. If
	// `window`, we query all metrics within the configured window, such that we retain the full
	// fidelity of the OxQL metrics. This also allows us to query OxQL less often without losing
	// resolution, and reduce load on OxQL.
	QueryMode QueryMode `mapstructure:"query_mode"`

	// QueryLookback configures the lookback interval of queries sent to the Oxide API. Only used
	// for the `last` query mode. Defaults to 5m.
	QueryLookback time.Duration `mapstructure:"query_lookback"`

	// QueryOffset is the offset applied to the end of the query window, only used for the `window`
	// query mode. Because samples can arrive in oximeter later than their recorded timestamp, we
	// include an offset so that late-arriving samples aren't dropped. Defaults to 5m.
	QueryOffset time.Duration `mapstructure:"query_offset"`

	// MaxWindowSize is the longest allowed query window, only used for the `window` query mode. If
	// the query window exceeds `MaxWindowSize`, e.g. to catch up after a restart or failed
	// collection, we left-truncate to avoid overwhelming oximeter.
	MaxWindowSize time.Duration `mapstructure:"max_window_size"`

	// AddLabels configures the receiver to add human-readable labels to metrics using the Oxide
	// API.
	AddLabels bool `mapstructure:"add_labels"`

	// AddUtilizationMetrics configures the receiver to add silo utilization metrics (cpu, memory,
	// disk) with provisioned and allocated values.
	AddUtilizationMetrics bool `mapstructure:"add_utilization_metrics"`

	// InsecureSkipVerify configures the receiver to skip TLS certificate verification when
	// connecting to the Oxide API.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// SchemaRefreshInterval configures the interval at which the receiver refreshes the list of
	// available metrics from the Oxide API.
	SchemaRefreshInterval time.Duration `mapstructure:"schema_refresh_interval"`
}

type QueryMode string

const (
	QueryModeLast   QueryMode = "last"
	QueryModeWindow QueryMode = "window"
)

func (m *QueryMode) UnmarshalText(text []byte) error {
	switch mode := QueryMode(text); mode {
	case QueryModeLast, QueryModeWindow:
		*m = mode
		return nil
	default:
		return fmt.Errorf(
			"invalid query mode %q, expected %q or %q",
			mode,
			QueryModeLast,
			QueryModeWindow,
		)
	}
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

	if cfg.QueryLookback < 0 {
		return fmt.Errorf("invalid query lookback %s", cfg.QueryOffset)
	}

	if cfg.QueryOffset < 0 {
		return fmt.Errorf("invalid query offset %s", cfg.QueryOffset)
	}

	if cfg.MaxWindowSize < 0 {
		return fmt.Errorf("invalid max window size %s", cfg.MaxWindowSize)
	}

	return nil
}
