package oxideauditlogsreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// TimeCompleted tracks the timestamp of the most recently processed log. We use this to filter
	// the start_time of the next log collection.
	TimeCompleted *time.Time `json:"time_completed,omitempty"`
	// ID tracks the id of the most recently processed log. Log timestamps aren't guaranteed unique,
	// and the start_time filter is inclusive, so we have to store the last observed id for
	// deduplication.
	ID string `json:"id,omitempty"`
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
	if s.cfg.CursorPath != "" {
		if loaded, ok := s.loadCursor(); ok {
			s.cursor = loaded
			s.logger.Info("audit log scraper started from cursor file",
				zap.Timep("start_time", loaded.TimeCompleted),
				zap.String("cursor_id", loaded.ID),
			)
		}
	}
	if s.cursor.TimeCompleted == nil {
		startTime := time.Now().Add(-s.cfg.InitialLookback)
		s.cursor = cursor{TimeCompleted: &startTime}
		s.logger.Info("audit log scraper started from initial lookback",
			zap.Time("start_time", startTime),
		)
	}

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
		StartTime: s.cursor.TimeCompleted,
		Limit:     &s.cfg.PageSize,
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
				s.saveCursor()
				return logs, scrapererror.NewPartialScrapeError(err, 0)
			}
			return logs, err
		}

		for _, entry := range page.Items {
			if entry.Id == s.cursor.ID {
				continue
			}

			if err := addLogRecord(logs, entry, startTime); err != nil {
				return logs, fmt.Errorf("adding log record: %w", err)
			}

			if entry.TimeCompleted != nil {
				s.cursor = cursor{
					TimeCompleted: entry.TimeCompleted,
					ID:            entry.Id,
				}
			}
		}

		s.logger.Debug("audit log page fetched",
			zap.Timep("cursor_time", s.cursor.TimeCompleted),
			zap.String("cursor_id", s.cursor.ID),
			zap.Int("items", len(page.Items)),
			zap.Int("total_logs", logs.LogRecordCount()),
			zap.Float64("latency", pageLatency),
		)

		if page.NextPage == "" {
			break
		}
		params.PageToken = page.NextPage
	}

	elapsed := time.Since(startTime)
	s.metrics.scrapeDuration.Record(ctx, elapsed.Seconds())
	s.metrics.scrapeCount.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))

	s.saveCursor()

	return logs, nil
}

// loadCursor reads and unmarshals the cursor file. Returns the cursor and true
// if successful, or a zero cursor and false on any error.
func (s *auditLogScraper) loadCursor() (cursor, bool) {
	data, err := os.ReadFile(s.cfg.CursorPath)
	if errors.Is(err, os.ErrNotExist) {
		s.logger.Info("cursor file not found, will use initial lookback",
			zap.String("path", s.cfg.CursorPath),
		)
		return cursor{}, false
	}
	if err != nil {
		s.logger.Warn("failed to read cursor file",
			zap.String("path", s.cfg.CursorPath),
			zap.Error(err),
		)
		return cursor{}, false
	}
	var c cursor
	if err := json.Unmarshal(data, &c); err != nil {
		s.logger.Warn(
			"failed to parse cursor file",
			zap.String("path", s.cfg.CursorPath),
			zap.Error(err),
		)
		return cursor{}, false
	}
	if c.TimeCompleted == nil {
		s.logger.Warn("cursor file missing time_completed", zap.String("path", s.cfg.CursorPath))
		return cursor{}, false
	}
	return c, true
}

// saveCursor writes the cursor to disk.
func (s *auditLogScraper) saveCursor() {
	if s.cfg.CursorPath == "" {
		return
	}
	data, err := json.Marshal(s.cursor)
	if err != nil {
		s.logger.Warn("failed to marshal cursor", zap.Error(err))
		return
	}
	dir := filepath.Dir(s.cfg.CursorPath)
	tmp, err := os.CreateTemp(dir, ".cursor-*.tmp")
	if err != nil {
		s.logger.Warn("failed to create temp cursor file", zap.Error(err))
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		s.logger.Warn("failed to write temp cursor file", zap.Error(err))
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("failed to close temp cursor file", zap.Error(err))
		return
	}
	if err := os.Rename(tmpName, s.cfg.CursorPath); err != nil {
		os.Remove(tmpName)
		s.logger.Warn("failed to rename cursor file", zap.Error(err))
		return
	}
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
