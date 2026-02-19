package oxideauditlogsreceiver

import (
	"context"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

// NewFactory creates a new factory for the oxide audit logs receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		component.MustNewType("oxideauditlogs"),
		createDefaultConfig,
		receiver.WithLogs(makeLogsReceiver, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	cfg := &Config{
		InitialLookback: 24 * time.Hour,
	}
	cfg.CollectionInterval = 60 * time.Second
	return cfg
}

func makeOxideClientOptions(cfg *Config) []oxide.ClientOption {
	var opts []oxide.ClientOption

	if cfg.Host != "" {
		opts = append(opts, oxide.WithHost(cfg.Host))
	}
	if cfg.Token != "" {
		opts = append(opts, oxide.WithToken(cfg.Token))
	}

	if cfg.InsecureSkipVerify {
		opts = append(opts, oxide.WithInsecureSkipVerify())
	}

	return opts
}

func makeLogsReceiver(
	_ context.Context,
	settings receiver.Settings,
	baseCfg component.Config,
	nextConsumer consumer.Logs,
) (receiver.Logs, error) {
	rCfg := baseCfg.(*Config)

	client, err := oxide.NewClient(makeOxideClientOptions(rCfg)...)
	if err != nil {
		return nil, err
	}

	s := newAuditLogScraper(rCfg, settings.TelemetrySettings, client)

	scraperFactory := scraper.NewFactory(
		component.MustNewType("oxideauditlogs"),
		func() component.Config { return &Config{} },
		scraper.WithLogs(
			func(
				_ context.Context,
				_ scraper.Settings,
				_ component.Config,
			) (scraper.Logs, error) {
				return scraper.NewLogs(s.Scrape, scraper.WithStart(s.Start))
			},
			component.StabilityLevelDevelopment,
		),
	)

	return scraperhelper.NewLogsController(
		&rCfg.ControllerConfig,
		settings,
		nextConsumer,
		scraperhelper.AddFactoryWithConfig(scraperFactory, rCfg),
	)
}
