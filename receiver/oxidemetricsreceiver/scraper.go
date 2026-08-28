package oxidemetricsreceiver

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type oxideScraper struct {
	client   *oxide.Client
	settings component.TelemetrySettings
	cfg      *Config
	logger   *zap.Logger
	host     string

	maxWindowSize time.Duration

	metricPatterns     []*regexp.Regexp
	schemasRefreshedAt time.Time
	metricNames        []string
	lastWindowEnd      time.Time

	apiRequestDuration     metric.Float64Gauge
	scrapeCount            metric.Int64Counter
	scrapeDuration         metric.Float64Gauge
	windowTruncateDuration metric.Float64Counter
}

func normalizeHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func newOxideScraper(
	cfg *Config,
	settings component.TelemetrySettings,
	client *oxide.Client,
) *oxideScraper {
	maxWindowSize := cfg.MaxWindowSize
	if maxWindowSize == 0 {
		maxWindowSize = 2 * cfg.CollectionInterval
	}

	return &oxideScraper{
		client:        client,
		settings:      settings,
		cfg:           cfg,
		logger:        settings.Logger,
		host:          normalizeHost(client.Host()),
		maxWindowSize: maxWindowSize,
	}
}

// ensureSchemas fetches metric schemas from the API if empty or stale. This ensures that the
// receiver discovers changes in metric schemas over time.
func (s *oxideScraper) ensureSchemas(ctx context.Context) error {
	now := time.Now()
	if len(s.metricNames) > 0 && s.schemasRefreshedAt.Add(s.cfg.SchemaRefreshInterval).After(now) {
		return nil
	}

	schemas, err := s.client.SystemTimeseriesSchemaListAllPages(
		ctx,
		oxide.SystemTimeseriesSchemaListParams{},
	)
	if err != nil {
		return err
	}

	metricNames := []string{}
	for _, schema := range schemas {
		for _, regexp := range s.metricPatterns {
			if regexp.MatchString(string(schema.TimeseriesName)) {
				metricNames = append(metricNames, string(schema.TimeseriesName))
			}
		}
	}

	s.logger.Info("collecting metrics", zap.Any("metrics", metricNames))

	s.metricNames = metricNames
	s.schemasRefreshedAt = now

	return nil
}

func (s *oxideScraper) Start(ctx context.Context, _ component.Host) error {
	regexps := []*regexp.Regexp{}
	for _, pattern := range s.cfg.MetricPatterns {
		regexp, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid metric pattern %s: %w", pattern, err)
		}
		regexps = append(regexps, regexp)
	}
	s.metricPatterns = regexps

	meter := s.settings.MeterProvider.Meter(
		"github.com/oxidecomputer/opentelemetry-collector-components/receiver/oxidemetricsreceiver",
	)

	var err error
	s.apiRequestDuration, err = meter.Float64Gauge(
		"oxide_receiver.api_request.duration",
		metric.WithDescription("Duration of API requests to the Oxide API"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create apiRequestDuration gauge: %w", err)
	}

	s.scrapeCount, err = meter.Int64Counter(
		"oxide_receiver.scrape.count",
		metric.WithDescription("Number of scrapes performed by the Oxide receiver"),
		metric.WithUnit("{scrape}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create scrapeCount counter: %w", err)
	}

	s.scrapeDuration, err = meter.Float64Gauge(
		"oxide_receiver.scrape.duration",
		metric.WithDescription("Total duration of the scrape operation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create scrapeDuration gauge: %w", err)
	}

	s.windowTruncateDuration, err = meter.Float64Counter(
		"oxide_receiver.scrape.window_truncate_duration",
		metric.WithDescription(
			"Duration the collection window was truncated due to exceeding the max window size",
		),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create windowTruncateDuration gauge: %w", err)
	}
	s.windowTruncateDuration.Add(
		ctx,
		0,
		metric.WithAttributes(attribute.String("oxide.host", s.host)),
	)

	return nil
}

func (s *oxideScraper) Shutdown(context.Context) error {
	return nil
}

func buildLastQuery(metricName string, lookback time.Duration) string {
	return fmt.Sprintf(
		"get %s | filter timestamp > @now() - %dms | last 1",
		metricName,
		lookback.Milliseconds(),
	)
}

func buildWindowQuery(metricName string, windowStart time.Time, windowEnd time.Time) string {
	return fmt.Sprintf(
		"get %s | filter timestamp > @%s | filter timestamp <= @%s",
		metricName,
		formatTimestamp(windowStart),
		formatTimestamp(windowEnd),
	)
}

func (s *oxideScraper) buildWindowBounds(
	startTime time.Time,
) (time.Time, time.Time, time.Duration) {
	collectionInterval := s.cfg.CollectionInterval
	maxWindowSize := s.maxWindowSize
	lastWindowEnd := s.lastWindowEnd

	windowEnd := startTime.Add(-s.cfg.QueryOffset)
	var truncated time.Duration

	if lastWindowEnd.IsZero() {
		lastWindowEnd = windowEnd.Add(-collectionInterval)
	} else if windowEnd.Sub(lastWindowEnd) > maxWindowSize {
		lastWindowEnd = windowEnd.Add(-maxWindowSize)
		truncated = lastWindowEnd.Sub(s.lastWindowEnd)
	}
	windowStart := lastWindowEnd

	return windowStart, windowEnd, truncated
}

func formatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000000")
}

func (s *oxideScraper) Scrape(ctx context.Context) (pmetric.Metrics, error) {
	metrics := pmetric.NewMetrics()

	var group errgroup.Group
	group.SetLimit(s.cfg.ScrapeConcurrency)

	if err := s.ensureSchemas(ctx); err != nil {
		return metrics, fmt.Errorf("refreshing metric schemas: %+w", err)
	}

	type queryResult struct {
		response *oxide.OxqlQueryResult
		latency  time.Duration
		err      error
	}
	results := make([]queryResult, len(s.metricNames))

	startTime := time.Now()
	windowStart, windowEnd, truncated := s.buildWindowBounds(startTime)
	if truncated > 0 {
		s.logger.Warn(
			"query window exceeds max_window_size, skipping ahead",
			zap.Duration("truncated", truncated),
			zap.Duration("max", s.maxWindowSize),
		)
		s.windowTruncateDuration.Add(
			ctx,
			truncated.Seconds(),
			metric.WithAttributes(attribute.String("oxide.host", s.host)),
		)
	}

	for idx, metricName := range s.metricNames {
		var query string
		if s.cfg.QueryMode == QueryModeLast {
			query = buildLastQuery(metricName, s.cfg.QueryLookback)
		} else {
			query = buildWindowQuery(metricName, windowStart, windowEnd)
		}
		group.Go(func() error {
			queryStartTime := time.Now()
			result, err := s.client.SystemTimeseriesQuery(ctx, oxide.SystemTimeseriesQueryParams{
				Body: &oxide.TimeseriesQuery{
					Query: query,
				},
			})
			elapsed := time.Since(queryStartTime)
			results[idx] = queryResult{
				response: result,
				latency:  elapsed,
				err:      err,
			}
			s.logger.Info(
				"scrape query finished",
				zap.String("metric", metricName),
				zap.String("query", query),
				zap.Float64("latency", elapsed.Seconds()),
			)
			if err != nil {
				s.logger.Warn(
					"failed to query metric",
					zap.String("metric", metricName),
					zap.Error(err),
				)
			} else {
				s.apiRequestDuration.Record(
					ctx,
					elapsed.Seconds(),
					metric.WithAttributes(
						attribute.String("request_name", metricName),
						attribute.String("oxide.host", s.host),
					),
				)
			}
			return nil
		})
	}

	// We don't check the return value of Wait(). Instead, we accumulate error counts in the
	// goroutine, and return a PartialScrapeError below if we observe >0 errors. Errors will be
	// surfaced to users via the `scraper_errored_metric_points_total` metric, and collector logs
	// contain the full details of failed scrapes.
	_ = group.Wait()
	elapsed := time.Since(startTime)
	s.logger.Info("scrape finished", zap.Float64("latency", elapsed.Seconds()))

	hostAttr := attribute.String("oxide.host", s.host)
	s.scrapeDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(hostAttr))
	s.scrapeCount.Add(ctx, 1, metric.WithAttributes(hostAttr))

	var queryErrors int
	for _, result := range results {
		if result.err != nil {
			queryErrors++
		}
	}

	// Cache mappings from resource UUIDs to human-readable names. Note: we can also add mappings
	// for higher-cardinality resources like instances and disks, but this would add more latency to
	// the 0th query on the page.
	//
	// TODO: add human-readable labels to metrics in oximeter so that we don't have to enrich them
	// here. Tracked in https://github.com/oxidecomputer/omicron/issues/9119.
	siloToName := map[string]string{}
	projectToName := map[string]string{}
	if s.cfg.AddLabels {
		silos, err := s.client.SiloListAllPages(ctx, oxide.SiloListParams{})
		if err != nil {
			return metrics, fmt.Errorf("listing silos: %w", err)
		}
		for _, silo := range silos {
			siloToName[silo.Id] = string(silo.Name)
		}
		// Note: this only lists projects in the silo corresponding to the client's authentication
		// token. In the future, we can either add a system endpoint listing all projects for the
		// rack, or enrich metrics with project labels in nexus.
		projects, err := s.client.ProjectListAllPages(ctx, oxide.ProjectListParams{})
		if err != nil {
			return metrics, fmt.Errorf("listing projects: %w", err)
		}
		for _, project := range projects {
			projectToName[project.Id] = string(project.Name)
		}
	}

	rm := metrics.ResourceMetrics().AppendEmpty()
	resource := rm.Resource()
	resource.Attributes().PutStr("service.name", "oxide")
	resource.Attributes().PutStr("oxide.host", s.host)
	sm := rm.ScopeMetrics().AppendEmpty()

	var parseErrors int
	for _, result := range results {
		if result.err != nil {
			continue
		}

		for _, table := range result.response.Tables {
			// Collect and validate non-empty timeseries, converting deltas to cumulative.
			var timeseries []oxide.Timeseries
			for _, series := range table.Timeseries {
				series, err := accumulate(series)
				if err != nil {
					s.logger.Warn(
						"failed to convert series to cumulative",
						zap.String("metric", table.Name),
						zap.Error(err),
					)
					parseErrors++
					continue
				}

				// All OxQL queries should return exactly one metric value, unless the query is
				// empty. OxQL only returns multiple values when the query includes a join, which
				// our queries do not.
				if len(series.Points.Values) == 0 {
					continue
				}
				if len(series.Points.Values) > 1 {
					s.logger.Warn(
						"expected exactly one metric value",
						zap.String("metric", table.Name),
						zap.Int("values", len(series.Points.Values)),
					)
					parseErrors++
					continue
				}

				timeseries = append(timeseries, series)
			}

			if len(timeseries) == 0 {
				continue
			}

			m := sm.Metrics().AppendEmpty()
			m.SetName(table.Name)

			// Determine the metric type from the first series. By this point, we've already ensured
			// that timeseries and timeseries[0].Points.Values are each non-empty.
			v0 := timeseries[0].Points.Values[0]

			if slices.Contains(
				[]oxide.ValueArrayType{
					oxide.ValueArrayTypeIntegerDistribution,
					oxide.ValueArrayTypeDoubleDistribution,
				},
				v0.Values.Type(),
			) {
				measure := m.SetEmptyHistogram()
				measure.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
				dataPoints := measure.DataPoints()
				for _, series := range timeseries {
					if err := addHistogram(
						dataPoints,
						table,
						series,
						series.Points.Values[0],
						siloToName,
						projectToName,
					); err != nil {
						s.logger.Warn(
							"failed to add histogram metric",
							zap.String("metric", table.Name),
							zap.Error(err),
						)
						parseErrors++
					}
				}
			} else {
				var dataPoints pmetric.NumberDataPointSlice
				if v0.MetricType == oxide.MetricTypeGauge {
					dataPoints = m.SetEmptyGauge().DataPoints()
				} else {
					measure := m.SetEmptySum()
					measure.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
					measure.SetIsMonotonic(true)
					dataPoints = measure.DataPoints()
				}
				for _, series := range timeseries {
					if err := addPoint(
						dataPoints,
						series,
						series.Points.Values[0],
						siloToName,
						projectToName,
					); err != nil {
						s.logger.Warn(
							"failed to add metric",
							zap.String("metric", table.Name),
							zap.Error(err),
						)
						parseErrors++
					}
				}
			}
		}
	}

	if s.cfg.AddUtilizationMetrics {
		if err := s.addSiloUtilization(ctx, metrics); err != nil {
			return metrics, fmt.Errorf("adding silo utilization metrics: %w", err)
		}
	}

	// Record the end of the last (at least partially) successful collection.
	if len(results) == 0 || queryErrors < len(results) {
		s.lastWindowEnd = windowEnd
	}

	// Propagate partial errors to the collector machinery.
	if queryErrors > 0 || parseErrors > 0 {
		return metrics, scrapererror.NewPartialScrapeError(
			fmt.Errorf("%d query errors, %d parse errors", queryErrors, parseErrors),
			queryErrors+parseErrors,
		)
	}

	return metrics, nil
}

// addSiloUtilization adds metrics for allocated and provisioned silo resources, including cpu,
// memory, and disk.
//
// TODO: Implement this via oximeter rather than deriving metrics from the API.
func (s *oxideScraper) addSiloUtilization(ctx context.Context, metrics pmetric.Metrics) error {
	resp, err := s.client.SiloUtilizationListAllPages(ctx, oxide.SiloUtilizationListParams{})
	if err != nil {
		return err
	}
	addSiloUtilizationMetrics(metrics, resp, pcommon.NewTimestampFromTime(time.Now()), s.host)
	return nil
}

// addSiloUtilizationMetrics adds silo utilization data to the metrics.
func addSiloUtilizationMetrics(
	metrics pmetric.Metrics,
	utilizations []oxide.SiloUtilization,
	timestamp pcommon.Timestamp,
	host string,
) {
	rm := metrics.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "oxide")
	rm.Resource().Attributes().PutStr("oxide.host", host)
	sm := rm.ScopeMetrics().AppendEmpty()

	addGauge := func(name string) pmetric.Gauge {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		return m.SetEmptyGauge()
	}

	cpuGauge := addGauge("silo_utilization.cpu")
	memoryGauge := addGauge("silo_utilization.memory")
	diskGauge := addGauge("silo_utilization.disk")

	for _, su := range utilizations {
		addDataPoint := func(gauge pmetric.Gauge, value int64, resourceType string) {
			dp := gauge.DataPoints().AppendEmpty()
			dp.SetTimestamp(timestamp)
			dp.SetIntValue(value)
			dp.Attributes().PutStr("silo_id", su.SiloId)
			dp.Attributes().PutStr("silo_name", string(su.SiloName))
			dp.Attributes().PutStr("type", resourceType)
		}

		for _, res := range []struct {
			counts       oxide.VirtualResourceCounts
			resourceType string
		}{
			{su.Provisioned, "provisioned"},
			{su.Allocated, "allocated"},
		} {
			cpus := int64(0)
			if res.counts.Cpus != nil {
				cpus = int64(*res.counts.Cpus)
			}
			addDataPoint(cpuGauge, cpus, res.resourceType)
			addDataPoint(memoryGauge, int64(res.counts.Memory), res.resourceType)
			addDataPoint(diskGauge, int64(res.counts.Storage), res.resourceType)
		}
	}
}

func addLabels(series oxide.Timeseries, attrs pcommon.Map) {
	for key, value := range series.Fields {
		switch v := value.Value.(type) {
		case *oxide.FieldValueString:
			attrs.PutStr(key, v.Value)
		case *oxide.FieldValueI8:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI16:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI32:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI64:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU8:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU16:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU32:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU64:
			attrs.PutInt(key, int64(*v.Value))
		case *oxide.FieldValueUuid:
			attrs.PutStr(key, v.Value)
		case *oxide.FieldValueIpAddr:
			attrs.PutStr(key, v.Value)
		case *oxide.FieldValueBool:
			attrs.PutBool(key, *v.Value)
		default:
			// Unreachable: if we get an unknown FieldValue variant, the SDK will return an error
			// from UnmarshalJSON.
			panic(fmt.Sprintf("unhandled FieldValue type: %T", value.Value))
		}
	}
}

func enrichLabels(attrs pcommon.Map, silos map[string]string, projects map[string]string) {
	if siloID, ok := attrs.Get("silo_id"); ok {
		if siloName, ok := silos[siloID.Str()]; ok {
			attrs.PutStr("silo_name", siloName)
		}
	}
	if projectID, ok := attrs.Get("project_id"); ok {
		if projectName, ok := projects[projectID.Str()]; ok {
			attrs.PutStr("project_name", projectName)
		}
	}
}

func addHistogram(
	dataPoints pmetric.HistogramDataPointSlice,
	table oxide.OxqlTable,
	series oxide.Timeseries,
	metricValue oxide.Values,
	silos map[string]string,
	projects map[string]string,
) error {
	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes

	switch v := metricValue.Values.Value.(type) {
	case *oxide.ValueArrayIntegerDistribution:
		if len(timestamps) != len(v.Values) {
			return fmt.Errorf(
				"invariant violated: number of timestamps %d must match number of values %d",
				len(timestamps),
				len(v.Values),
			)
		}
		for pointIdx, distValue := range v.Values {
			if distValue == nil {
				continue
			}
			if len(distValue.Bins) == 0 {
				continue
			}

			dp := dataPoints.AppendEmpty()
			dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[pointIdx]))
			if len(startTimes) > 0 {
				dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[pointIdx]))
			}

			// OxQL histograms model bins using a slice where each value represents the lower bound
			// of the bin. A slice like [0, 10, 20, 30, 40] represents five bins: [0, 10), [10, 20),
			// [20, 30), [30, 40), and [40, ∞). OpenTelemetry histograms represent buckets
			// differently: each value represents the upper bound of the bin, and there's an implied
			// bin that captures values greater than the final value of the slice. OpenTelemetry
			// models the histogram above as [10, 20, 30, 40]. To make these representations match,
			// we just drop the 0th OxQL bin and use the result as the OpenTelemetry bins. Note also
			// that OxQL bins are closed at their lower bound and open at their upper bound. e.g.
			// [0, 10). OpenTelemetry bins are open at the lower bound and closed at the upper
			// bound: (0, 10]. It's not possible to reconstruct that boundary behavior while
			// converting from OxQL histograms to OpenTelemetry histograms, so we accept the
			// difference.
			bins := make([]float64, len(distValue.Bins)-1)
			for binIdx, binValue := range distValue.Bins[1:] {
				bins[binIdx] = float64(binValue)
			}
			dp.ExplicitBounds().FromRaw(bins)

			counts := dp.BucketCounts()
			var total uint64
			for _, count := range distValue.Counts {
				counts.Append(count)
				total += count
			}
			dp.SetCount(total)

			addLabels(series, dp.Attributes())
			enrichLabels(dp.Attributes(), silos, projects)
		}
	case *oxide.ValueArrayDoubleDistribution:
		if len(timestamps) != len(v.Values) {
			return fmt.Errorf(
				"invariant violated: number of timestamps %d must match number of values %d",
				len(timestamps),
				len(v.Values),
			)
		}
		for idx, distValue := range v.Values {
			if distValue == nil {
				continue
			}
			if len(distValue.Bins) == 0 {
				continue
			}

			dp := dataPoints.AppendEmpty()
			dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
			if len(startTimes) > 0 {
				dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
			}

			dp.ExplicitBounds().FromRaw(distValue.Bins[1:])
			counts := dp.BucketCounts()
			var total uint64
			for _, count := range distValue.Counts {
				counts.Append(count)
				total += count
			}
			dp.SetCount(total)

			addLabels(series, dp.Attributes())
			enrichLabels(dp.Attributes(), silos, projects)
		}
	default:
		return fmt.Errorf(
			"unexpected histogram type %T for metric %s",
			metricValue.Values.Value,
			table.Name,
		)
	}
	return nil
}

func addPoint(
	dataPoints pmetric.NumberDataPointSlice,
	series oxide.Timeseries,
	metricValue oxide.Values,
	silos map[string]string,
	projects map[string]string,
) error {
	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes
	hasStartTimes := len(startTimes) > 0
	switch v := metricValue.Values.Value.(type) {
	case *oxide.ValueArrayInteger:
		if len(timestamps) != len(v.Values) {
			return fmt.Errorf(
				"invariant violated: number of timestamps %d must match number of values %d",
				len(timestamps),
				len(v.Values),
			)
		}
		for idx, intValue := range v.Values {
			// OxQL can emit null values for metrics queries. We don't have an obvious way to
			// represent these values in OTLP, and don't want to replace missing values with zero
			// values, so simply omit the datapoint entirely in this case.
			if intValue != nil {
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				dp.SetIntValue(int64(*intValue))
				addLabels(series, dp.Attributes())
				enrichLabels(dp.Attributes(), silos, projects)
			}
		}
	case *oxide.ValueArrayDouble:
		if len(timestamps) != len(v.Values) {
			return fmt.Errorf(
				"invariant violated: number of timestamps %d must match number of values %d",
				len(timestamps),
				len(v.Values),
			)
		}
		for idx, floatValue := range v.Values {
			if floatValue != nil {
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				dp.SetDoubleValue(*floatValue)
				addLabels(series, dp.Attributes())
				enrichLabels(dp.Attributes(), silos, projects)
			}
		}
	case *oxide.ValueArrayBoolean:
		if len(timestamps) != len(v.Values) {
			return fmt.Errorf(
				"invariant violated: number of timestamps %d must match number of values %d",
				len(timestamps),
				len(v.Values),
			)
		}
		for idx, boolValue := range v.Values {
			if boolValue != nil {
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				intValue := 0
				if *boolValue {
					intValue = 1
				}
				dp.SetIntValue(int64(intValue))
				addLabels(series, dp.Attributes())
				enrichLabels(dp.Attributes(), silos, projects)
			}
		}
	default:
		return fmt.Errorf("got unexpected metric value type %T", metricValue.Values.Value)
	}
	return nil
}
