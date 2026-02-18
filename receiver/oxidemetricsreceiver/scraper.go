package oxidemetricsreceiver

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
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

	metricNames []string

	apiRequestDuration metric.Float64Gauge
	scrapeCount        metric.Int64Counter
	scrapeDuration     metric.Float64Gauge
	metricParseErrors  metric.Int64Counter
}

func newOxideScraper(
	cfg *Config,
	settings component.TelemetrySettings,
	client *oxide.Client,
) *oxideScraper {
	return &oxideScraper{
		client:   client,
		settings: settings,
		cfg:      cfg,
		logger:   settings.Logger,
	}
}

func (s *oxideScraper) Start(ctx context.Context, _ component.Host) error {
	schemas, err := s.client.SystemTimeseriesSchemaListAllPages(
		ctx,
		oxide.SystemTimeseriesSchemaListParams{},
	)
	if err != nil {
		return err
	}

	regexps := []*regexp.Regexp{}
	for _, pattern := range s.cfg.MetricPatterns {
		regexp, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid metric pattern %s: %w", pattern, err)
		}
		regexps = append(regexps, regexp)
	}

	metricNames := []string{}
	for _, schema := range schemas {
		for _, regexp := range regexps {
			if regexp.MatchString(string(schema.TimeseriesName)) {
				metricNames = append(metricNames, string(schema.TimeseriesName))
			}
		}
	}
	s.metricNames = metricNames

	s.logger.Info("collecting metrics", zap.Any("metrics", metricNames))

	meter := s.settings.MeterProvider.Meter(
		"github.com/oxidecomputer/opentelemetry-collector-components/receiver/oxidemetricsreceiver",
	)

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

	s.metricParseErrors, err = meter.Int64Counter(
		"oxide_receiver.metric.parse_errors",
		metric.WithDescription("Number of errors encountered while parsing individual metrics"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return fmt.Errorf("failed to create metricParseErrors counter: %w", err)
	}

	return nil
}

func (s *oxideScraper) Shutdown(context.Context) error {
	return nil
}

func (s *oxideScraper) Scrape(ctx context.Context) (pmetric.Metrics, error) {
	metrics := pmetric.NewMetrics()

	var group errgroup.Group
	group.SetLimit(s.cfg.ScrapeConcurrency)

	startTime := time.Now()
	results := make([]*oxide.OxqlQueryResult, len(s.metricNames))

	latencies := make([]time.Duration, len(s.metricNames))

	for idx, metricName := range s.metricNames {
		query := fmt.Sprintf(
			"get %s | filter timestamp > @now() - %s | last 1",
			metricName,
			s.cfg.QueryLookback,
		)
		group.Go(func() error {
			goroStartTime := time.Now()
			result, err := s.client.SystemTimeseriesQuery(ctx, oxide.SystemTimeseriesQueryParams{
				Body: &oxide.TimeseriesQuery{
					Query: query,
				},
			})
			elapsed := time.Since(goroStartTime)
			latencies[idx] = elapsed
			s.logger.Info(
				"scrape query finished",
				zap.String("metric", metricName),
				zap.String("query", query),
				zap.Float64("latency", elapsed.Seconds()),
			)
			if err != nil {
				return err
			}
			results[idx] = result
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		s.scrapeCount.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "failure")))
		return metrics, err
	}
	elapsed := time.Since(startTime)
	s.logger.Info("scrape finished", zap.Float64("latency", elapsed.Seconds()))

	s.scrapeDuration.Record(ctx, elapsed.Seconds())
	s.scrapeCount.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "success")))

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

	for _, result := range results {
		for _, table := range result.Tables {
			for _, series := range table.Timeseries {
				rm := metrics.ResourceMetrics().AppendEmpty()
				resource := rm.Resource()

				addLabels(series, resource)

				enrichLabels(resource, siloToName, projectToName)

				var sm pmetric.ScopeMetrics
				if rm.ScopeMetrics().Len() == 0 {
					sm = rm.ScopeMetrics().AppendEmpty()
				} else {
					sm = rm.ScopeMetrics().At(0)
				}

				m := sm.Metrics().AppendEmpty()

				m.SetName(table.Name)

				// Hack: get metadata from the 0th point.
				// TODO(jmcarp): Move this to the timeseries level in the api.
				if len(series.Points.Values) == 0 {
					continue
				}
				v0 := series.Points.Values[0]

				switch {
				// Handle histograms.
				//
				// Note: OxQL histograms include both buckets and counts, as well as a handful of
				// preselected quantiles estimated using the P² algorithm. We extract the buckets
				// and counts as an otel histogram, and the quantiles as a gauge.
				case slices.Contains([]oxide.ValueArrayType{oxide.ValueArrayTypeIntegerDistribution, oxide.ValueArrayTypeDoubleDistribution}, v0.Values.Type()):
					measure := m.SetEmptyHistogram()
					// Always set aggregation temporality to cumulative. OxQL has both delta and
					// cumulative counters, but both counter types use a cumulative value for their
					// 0th observation. Because we add "| last 1" to all OxQL queries, all counter
					// metrics are effectively of type cumulative for our purposes.
					measure.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

					quantiles := sm.Metrics().AppendEmpty()
					quantiles.SetName(fmt.Sprintf("%s:quantiles", table.Name))
					quantileGauge := quantiles.SetEmptyGauge()

					if err := addHistogram(
						measure.DataPoints(),
						quantileGauge,
						table,
						series,
					); err != nil {
						s.logger.Warn(
							"failed to add histogram metric",
							zap.String("metric", table.Name),
							zap.Error(err),
						)
						s.metricParseErrors.Add(ctx, 1, metric.WithAttributes(
							attribute.String("metric_name", table.Name),
						))
					}
				// Handle scalar gauge.
				case v0.MetricType == oxide.MetricTypeGauge:
					measure := m.SetEmptyGauge()
					if err := addPoint(measure.DataPoints(), series); err != nil {
						s.logger.Warn(
							"failed to add gauge metric",
							zap.String("metric", table.Name),
							zap.Error(err),
						)
						s.metricParseErrors.Add(ctx, 1, metric.WithAttributes(
							attribute.String("metric_name", table.Name),
						))
					}

				// Handle scalar counter.
				default:
					measure := m.SetEmptySum()
					// Always set aggregation temporality to cumulative. OxQL has both delta and
					// cumulative counters, but both counter types use a cumulative value for their
					// 0th observation. Because we add "| last 1" to all OxQL queries, all counter
					// metrics are effectively of type cumulative for our purposes.
					measure.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
					measure.SetIsMonotonic(true)

					if err := addPoint(measure.DataPoints(), series); err != nil {
						s.logger.Warn(
							"failed to add sum metric",
							zap.String("metric", table.Name),
							zap.Error(err),
						)
						s.metricParseErrors.Add(ctx, 1, metric.WithAttributes(
							attribute.String("metric_name", table.Name),
						))
					}
				}

			}
		}
	}

	for idx, metricName := range s.metricNames {
		s.apiRequestDuration.Record(
			ctx,
			latencies[idx].Seconds(),
			metric.WithAttributes(attribute.String("request_name", metricName)),
		)
	}

	if s.cfg.AddUtilizationMetrics {
		if err := s.addSiloUtilization(ctx, metrics); err != nil {
			return metrics, fmt.Errorf("adding silo utilization metrics: %w", err)
		}
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
	addSiloUtilizationMetrics(metrics, resp, pcommon.NewTimestampFromTime(time.Now()))
	return nil
}

// addSiloUtilizationMetrics adds silo utilization data to the metrics.
func addSiloUtilizationMetrics(
	metrics pmetric.Metrics,
	utilizations []oxide.SiloUtilization,
	timestamp pcommon.Timestamp,
) {
	rm := metrics.ResourceMetrics().AppendEmpty()
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

func addLabels(series oxide.Timeseries, resource pcommon.Resource) {
	for key, value := range series.Fields {
		switch v := value.Value.(type) {
		case *oxide.FieldValueString:
			resource.Attributes().PutStr(key, v.Value)
		case *oxide.FieldValueI8:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI16:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI32:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueI64:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU8:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU16:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU32:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueU64:
			resource.Attributes().PutInt(key, int64(*v.Value))
		case *oxide.FieldValueUuid:
			resource.Attributes().PutStr(key, v.Value)
		case *oxide.FieldValueIpAddr:
			resource.Attributes().PutStr(key, v.Value)
		case *oxide.FieldValueBool:
			resource.Attributes().PutBool(key, *v.Value)
		default:
			// Unreachable: if we get an unknown FieldValue variant, the SDK will return an error
			// from UnmarshalJSON.
			panic(fmt.Sprintf("unhandled FieldValue type: %T", value.Value))
		}
	}
}

func enrichLabels(resource pcommon.Resource, silos map[string]string, projects map[string]string) {
	if siloID, ok := resource.Attributes().Get("silo_id"); ok {
		if siloName, ok := silos[siloID.Str()]; ok {
			resource.Attributes().PutStr("silo_name", siloName)
		}
	}
	if projectID, ok := resource.Attributes().Get("project_id"); ok {
		if projectName, ok := projects[projectID.Str()]; ok {
			resource.Attributes().PutStr("project_name", projectName)
		}
	}
}

func addHistogram(
	dataPoints pmetric.HistogramDataPointSlice,
	quantileGauge pmetric.Gauge,
	table oxide.OxqlTable,
	series oxide.Timeseries,
) error {
	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes
	for idx, point := range series.Points.Values {
		dp := dataPoints.AppendEmpty()
		dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
		if len(startTimes) > 0 {
			dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
		}

		switch v := point.Values.Value.(type) {
		case *oxide.ValueArrayIntegerDistribution:
			if len(timestamps) != len(v.Values) {
				return fmt.Errorf(
					"invariant violated: number of timestamps %d must match number of values %d",
					len(timestamps),
					len(v.Values),
				)
			}
			for _, distValue := range v.Values {
				bins := make([]float64, len(distValue.Bins))
				for i := range distValue.Bins {
					bins[i] = float64(distValue.Bins[i])
				}
				dp.ExplicitBounds().FromRaw(bins)

				counts := dp.BucketCounts()
				total := 0
				for _, count := range distValue.Counts {
					counts.Append(uint64(count))
					total += count
				}
				dp.SetCount(uint64(total))
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))

				addQuantiles(
					quantileGauge,
					distValue.P50,
					distValue.P90,
					distValue.P99,
					dp.Timestamp(),
				)
			}
		case *oxide.ValueArrayDoubleDistribution:
			if len(timestamps) != len(v.Values) {
				return fmt.Errorf(
					"invariant violated: number of timestamps %d must match number of values %d",
					len(timestamps),
					len(v.Values),
				)
			}
			for _, distValue := range v.Values {
				dp.ExplicitBounds().FromRaw(distValue.Bins)

				counts := dp.BucketCounts()
				total := 0
				for _, count := range distValue.Counts {
					counts.Append(uint64(count))
					total += count
				}
				dp.SetCount(uint64(total))
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))

				addQuantiles(
					quantileGauge,
					distValue.P50,
					distValue.P90,
					distValue.P99,
					dp.Timestamp(),
				)
			}
		default:
			return fmt.Errorf(
				"unexpected histogram type %T for metric %s",
				point.Values.Value,
				table.Name,
			)
		}
	}
	return nil
}

func addPoint(dataPoints pmetric.NumberDataPointSlice, series oxide.Timeseries) error {
	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes
	hasStartTimes := len(startTimes) > 0
	for _, point := range series.Points.Values {
		switch v := point.Values.Value.(type) {
		case *oxide.ValueArrayInteger:
			if len(timestamps) != len(v.Values) {
				return fmt.Errorf(
					"invariant violated: number of timestamps %d must match number of values %d",
					len(timestamps),
					len(v.Values),
				)
			}
			for idx, intValue := range v.Values {
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				dp.SetIntValue(int64(intValue))
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
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				dp.SetDoubleValue(floatValue)
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
				dp := dataPoints.AppendEmpty()
				dp.SetTimestamp(pcommon.NewTimestampFromTime(timestamps[idx]))
				if hasStartTimes {
					dp.SetStartTimestamp(pcommon.NewTimestampFromTime(startTimes[idx]))
				}
				intValue := 0
				if boolValue {
					intValue = 1
				}
				dp.SetIntValue(int64(intValue))
			}
		default:
			return fmt.Errorf("got unexpected metric value type %T", point.Values.Value)
		}
	}
	return nil
}

// addQuantiles emits metrics for P50, P90, P99 quantile values. In addition
// to histogram buckets and counts, OxQL exposes a set of predefined quantile
// estimates using the P² algorithm, which we extract here.
func addQuantiles(g pmetric.Gauge, p50, p90, p99 float64, timestamp pcommon.Timestamp) {
	for _, q := range []struct {
		p     float64
		value float64
	}{
		{0.50, p50},
		{0.90, p90},
		{0.99, p99},
	} {
		p := g.DataPoints().AppendEmpty()
		p.SetTimestamp(timestamp)
		p.SetDoubleValue(q.value)
		p.Attributes().PutDouble("quantile", q.p)
	}
}
