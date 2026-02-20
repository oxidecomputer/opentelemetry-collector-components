package oxideauditlogsreceiver

import (
	"time"

	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`

	Host  string `mapstructure:"host"`
	Token string `mapstructure:"token"`

	// InitialLookback configures how far back to query on the first scrape.
	InitialLookback time.Duration `mapstructure:"initial_lookback"`

	// InsecureSkipVerify configures the receiver to skip TLS certificate
	// verification when connecting to the Oxide API.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	// CursorPath is an optional file path for persisting the pagination cursor
	// across restarts. If empty, the cursor is only held in memory and the
	// receiver falls back to InitialLookback on restart.
	CursorPath string `mapstructure:"cursor_path"`

	// PageSize controls the number of audit log entries fetched per API request.
	PageSize int `mapstructure:"page_size"`
}

func (cfg *Config) Validate() error {
	return nil
}
