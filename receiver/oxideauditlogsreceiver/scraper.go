package oxideauditlogsreceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// cursor tracks the high-water mark for resuming audit log pagination.
type cursor struct {
	// timeCompleted tracks the timestamp of the most recently processed log. We use this to filter
	// the start_time of the next log collection.
	timeCompleted *time.Time
	// id tracks the id of the most recently processed log. Log timestamps aren't guaranteed unique,
	// and the start_time filter is inclusive, so we have to store the last observed id for
	// deduplication.
	id string
}

// auditLogClient is the type of the Oxide audit log fetcher. We use an interface for testing
// purposes.
type auditLogClient interface {
	AuditLogList(ctx context.Context, params oxide.AuditLogListParams) (
		*oxide.AuditLogEntryResultsPage, error,
	)
}

// scraperMetrics holds metrics about the log scraper.
type scraperMetrics struct {
	scrapeCount        metric.Int64Counter
	scrapeDuration     metric.Float64Gauge
	apiRequestDuration metric.Float64Gauge
}

type auditLogScraper struct {
	client   auditLogClient
	cfg      *Config
	settings component.TelemetrySettings
	logger   *zap.Logger
	metrics  scraperMetrics
	cursor   cursor
}

func newAuditLogScraper(
	cfg *Config,
	settings component.TelemetrySettings,
	client auditLogClient,
) *auditLogScraper {
	return &auditLogScraper{
		client:   client,
		cfg:      cfg,
		settings: settings,
		logger:   settings.Logger,
	}
}

func (s *auditLogScraper) Start(_ context.Context, _ component.Host) error {
	startTime := time.Now().Add(-s.cfg.InitialLookback)
	s.cursor = cursor{timeCompleted: &startTime}
	s.logger.Info("audit log scraper started",
		zap.Time("start_time", startTime),
	)

	meter := s.settings.MeterProvider.Meter(
		"github.com/oxidecomputer/opentelemetry-collector-components/receiver/oxideauditlogsreceiver",
	)

	var err error
	s.metrics.scrapeCount, err = meter.Int64Counter(
		"oxide_audit_logs_receiver.scrape.count",
		metric.WithDescription("Number of scrapes performed by the audit log receiver"),
		metric.WithUnit("{scrape}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create scrapeCount counter: %w", err)
	}

	s.metrics.scrapeDuration, err = meter.Float64Gauge(
		"oxide_audit_logs_receiver.scrape.duration",
		metric.WithDescription("Total duration of the scrape operation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create scrapeDuration gauge: %w", err)
	}

	s.metrics.apiRequestDuration, err = meter.Float64Gauge(
		"oxide_audit_logs_receiver.api_request.duration",
		metric.WithDescription("Duration of API requests to the Oxide API"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create apiRequestDuration gauge: %w", err)
	}

	return nil
}

func (s *auditLogScraper) Scrape(ctx context.Context) (plog.Logs, error) {
	logs := plog.NewLogs()
	startTime := time.Now()

	params := oxide.AuditLogListParams{
		StartTime: s.cursor.timeCompleted,
		SortBy:    oxide.TimeAndIdSortModeTimeAndIdAscending,
	}

	// Iterate over pages rather than using AuditLogListAllPages. We may have many pages to inspect,
	// and we want to return partial results in case we fail partway through.
	for {
		pageStart := time.Now()
		page, err := s.client.AuditLogList(ctx, params)
		pageLatency := time.Since(pageStart).Seconds()
		s.metrics.apiRequestDuration.Record(ctx, pageLatency)
		if err != nil {
			s.logger.Warn("audit log list request failed", zap.Error(err))
			s.metrics.scrapeCount.Add(
				ctx,
				1,
				metric.WithAttributes(attribute.String("status", "failure")),
			)
			// Emit partial results on error.
			if logs.LogRecordCount() > 0 {
				return logs, scrapererror.NewPartialScrapeError(err, 0)
			}
			return logs, err
		}

		s.logger.Debug("audit log page fetched",
			zap.Int("items", len(page.Items)),
			zap.Int("total_logs", logs.LogRecordCount()),
			zap.Float64("latency", pageLatency),
		)

		for _, entry := range page.Items {
			if entry.Id == s.cursor.id {
				continue
			}

			if err := addLogRecord(logs, entry, startTime); err != nil {
				return logs, fmt.Errorf("adding log record: %w", err)
			}

			if entry.TimeCompleted != nil {
				s.cursor = cursor{
					timeCompleted: entry.TimeCompleted,
					id:            entry.Id,
				}
			}
		}

		if page.NextPage == "" {
			break
		}
		params.PageToken = page.NextPage
	}

	elapsed := time.Since(startTime)
	s.metrics.scrapeDuration.Record(ctx, elapsed.Seconds())
	s.metrics.scrapeCount.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))

	return logs, nil
}

func addLogRecord(logs plog.Logs, entry oxide.AuditLogEntry, observedTime time.Time) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit log entry %s: %w", entry.Id, err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("unmarshaling audit log entry %s: %w", entry.Id, err)
	}

	resource := logs.ResourceLogs().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "oxide")

	scope := resource.ScopeLogs().AppendEmpty()
	log := scope.LogRecords().AppendEmpty()

	if entry.TimeStarted != nil {
		log.SetTimestamp(pcommon.NewTimestampFromTime(*entry.TimeStarted))
	}
	log.SetObservedTimestamp(pcommon.NewTimestampFromTime(observedTime))

	if err := log.Body().SetEmptyMap().FromRaw(body); err != nil {
		return fmt.Errorf("setting body for audit log entry %s: %w", entry.Id, err)
	}

	return nil
}
