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
}

func (cfg *Config) Validate() error {
	return nil
}
