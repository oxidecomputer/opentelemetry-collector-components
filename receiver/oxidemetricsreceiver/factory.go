package oxidemetricsreceiver

import (
	"context"

	"github.com/oxidecomputer/oxide.go/oxide"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

// NewFactory creates a new factory for the oxide metrics receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		component.MustNewType("oxide"),
		createDefaultConfig,
		receiver.WithMetrics(makeMetricsReceiver, component.StabilityLevelDevelopment),
	)
}

// createDefaultConfig creates the default configuration for the receiver.
func createDefaultConfig() component.Config {
	return &Config{
		MetricPatterns:    []string{".*"},
		ScrapeConcurrency: 16,
		QueryLookback:     "5m",
	}
}

// makeOxideClientOptions creates an oxide.ClientOption slice from the receiver Config.
func makeOxideClientOptions(cfg *Config) []oxide.ClientOption {
	var opts []oxide.ClientOption

	// Set `Host` and `Token` if defined; setting to empty strings would override the relevant
	// environment variables in the SDK.
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

// makeMetricsReceiver creates a metrics receiver based on the provided config.
func makeMetricsReceiver(
	ctx context.Context,
	settings receiver.Settings,
	baseCfg component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	rCfg := baseCfg.(*Config)

	client, err := oxide.NewClient(makeOxideClientOptions(rCfg)...)
	if err != nil {
		return nil, err
	}

	r := newOxideScraper(rCfg, settings.TelemetrySettings, client)
	s, err := scraper.NewMetrics(
		r.Scrape,
		scraper.WithStart(r.Start),
		scraper.WithShutdown(r.Shutdown),
	)
	if err != nil {
		return nil, err
	}

	return scraperhelper.NewMetricsController(
		&rCfg.ControllerConfig,
		settings,
		consumer,
		scraperhelper.AddScraper(component.MustNewType("oxide"), s),
	)
}
