package oxidereceiver

import (
	"testing"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatatest/pmetrictest"
	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

func TestAddPoint(t *testing.T) {
	logger := zap.NewNop()
	now := time.Now()
	table := oxide.OxqlTable{Name: "test_metric"}

	for _, tc := range []struct {
		name        string
		series      oxide.Timeseries
		wantMetrics []pmetric.NumberDataPoint
		wantErr     string
	}{
		// Integer test cases
		{
			name: "integer value: success",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeInteger,
								Values: []any{float64(42)},
							},
						},
					},
				},
			},
			wantMetrics: []pmetric.NumberDataPoint{
				func() pmetric.NumberDataPoint {
					dp := pmetric.NewNumberDataPoint()
					dp.SetIntValue(42)
					dp.SetTimestamp(pcommon.NewTimestampFromTime(now))
					return dp
				}(),
			},
		},
		{
			name: "integer value: type assertion error on outer array",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeInteger,
								Values: "not an array",
							},
						},
					},
				},
			},
			wantErr: "couldn't cast values",
		},
		{
			name: "integer value: type assertion error on value",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeInteger,
								Values: []any{"not a number"},
							},
						},
					},
				},
			},
			wantErr: "couldn't cast value",
		},
		// Double/Float test cases
		{
			name: "double value: success",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeDouble,
								Values: []any{float64(42.5)},
							},
						},
					},
				},
			},
			wantMetrics: []pmetric.NumberDataPoint{
				func() pmetric.NumberDataPoint {
					dp := pmetric.NewNumberDataPoint()
					dp.SetDoubleValue(42.5)
					dp.SetTimestamp(pcommon.NewTimestampFromTime(now))
					return dp
				}(),
			},
		},
		{
			name: "double value: type assertion error on outer array",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeDouble,
								Values: "not an array",
							},
						},
					},
				},
			},
			wantErr: "couldn't cast values",
		},
		{
			name: "double value: type assertion error on value",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeDouble,
								Values: []any{"not a number"},
							},
						},
					},
				},
			},
			wantErr: "couldn't cast value",
		},
		// Boolean test cases
		{
			name: "boolean value: success true",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeBoolean,
								Values: []any{true},
							},
						},
					},
				},
			},
			wantMetrics: []pmetric.NumberDataPoint{
				func() pmetric.NumberDataPoint {
					dp := pmetric.NewNumberDataPoint()
					dp.SetIntValue(1)
					dp.SetTimestamp(pcommon.NewTimestampFromTime(now))
					return dp
				}(),
			},
		},
		{
			name: "boolean value: success false",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeBoolean,
								Values: []any{false},
							},
						},
					},
				},
			},
			wantMetrics: []pmetric.NumberDataPoint{
				func() pmetric.NumberDataPoint {
					dp := pmetric.NewNumberDataPoint()
					dp.SetIntValue(0)
					dp.SetTimestamp(pcommon.NewTimestampFromTime(now))
					return dp
				}(),
			},
		},
		{
			name: "boolean value: type assertion error on outer array",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeBoolean,
								Values: "not an array",
							},
						},
					},
				},
			},
			wantErr: "couldn't cast values",
		},
		{
			name: "boolean value: type assertion error on value",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{now},
					Values: []oxide.Values{
						{
							Values: oxide.ValueArray{
								Type:   oxide.ValueArrayTypeBoolean,
								Values: []any{"not a boolean"},
							},
						},
					},
				},
			},
			wantErr: "couldn't cast value",
		},
		// Empty array
		{
			name: "empty values array",
			series: oxide.Timeseries{
				Points: oxide.Points{
					Timestamps: []time.Time{},
					Values:     []oxide.Values{},
				},
			},
			wantMetrics: []pmetric.NumberDataPoint{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pointFactory := func() pmetric.NumberDataPoint {
				metrics := pmetric.NewMetrics()
				rm := metrics.ResourceMetrics().AppendEmpty()
				sm := rm.ScopeMetrics().AppendEmpty()
				m := sm.Metrics().AppendEmpty()
				gauge := m.SetEmptyGauge()
				return gauge.DataPoints().AppendEmpty()
			}

			points, err := addPoint(pointFactory, table, tc.series, logger)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, points, len(tc.wantMetrics))

			for idx, wantMetric := range tc.wantMetrics {
				err := pmetrictest.CompareNumberDataPoint(wantMetric, points[idx])
				require.NoError(t, err, "mismatch at index %d", idx)
			}
		})
	}
}
