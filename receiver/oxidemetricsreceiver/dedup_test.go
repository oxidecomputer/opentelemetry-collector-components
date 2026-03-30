package oxidemetricsreceiver

import (
	"testing"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
)

func TestDedup_NoDuplicates(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epochA, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeGauge,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{Values: []float64{1.0, 2.0, 3.0}},
					},
				},
			},
		},
	}

	got := dedup(series)
	require.Equal(t, 3, len(got.Points.Timestamps))
	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.Equal(t, []float64{1.0, 2.0, 3.0}, v.Values)
}

func TestDedup_SubMillisecondDuplicates(t *testing.T) {
	// Three readings within the same millisecond, then one in the next.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts0 := base.Add(100 * time.Microsecond)
	ts1 := base.Add(200 * time.Microsecond)
	ts2 := base.Add(300 * time.Microsecond)
	ts3 := base.Add(1 * time.Millisecond)

	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{ts0, ts1, ts2, ts3},
			StartTimes: []time.Time{epochA, epochA, epochA, epochA},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeGauge,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{Values: []float64{10.0, 20.0, 30.0, 40.0}},
					},
				},
			},
		},
	}

	got := dedup(series)

	// Should keep last of the first ms (30.0) and the one in the next ms (40.0).
	require.Equal(t, 2, len(got.Points.Timestamps))
	require.Equal(t, ts2, got.Points.Timestamps[0])
	require.Equal(t, ts3, got.Points.Timestamps[1])

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.Equal(t, []float64{30.0, 40.0}, v.Values)

	require.Equal(t, 2, len(got.Points.StartTimes))
}

func TestDedup_SinglePoint(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeGauge,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []int{42}},
					},
				},
			},
		},
	}

	got := dedup(series)
	require.Equal(t, 1, len(got.Points.Timestamps))
}

func TestDedup_Empty(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{},
	}

	got := dedup(series)
	require.Equal(t, 0, len(got.Points.Timestamps))
}

func TestDedup_IntegerDistribution(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts0 := base.Add(100 * time.Microsecond)
	ts1 := base.Add(200 * time.Microsecond)
	ts2 := base.Add(1 * time.Millisecond)

	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{ts0, ts1, ts2},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayIntegerDistribution{
							Values: []oxide.Distributionint64{
								{Bins: []int{10}, Counts: []uint64{1, 2}},
								{Bins: []int{10}, Counts: []uint64{3, 4}},
								{Bins: []int{10}, Counts: []uint64{5, 6}},
							},
						},
					},
				},
			},
		},
	}

	got := dedup(series)
	require.Equal(t, 2, len(got.Points.Timestamps))

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayIntegerDistribution)
	// Keep last of first ms ([3,4]) and the one in next ms ([5,6])
	require.Equal(t, []uint64{3, 4}, v.Values[0].Counts)
	require.Equal(t, []uint64{5, 6}, v.Values[1].Counts)
}

func TestDedup_NoStartTimes(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts0 := base.Add(100 * time.Microsecond)
	ts1 := base.Add(200 * time.Microsecond)

	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{ts0, ts1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeGauge,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{Values: []float64{1.0, 2.0}},
					},
				},
			},
		},
	}

	got := dedup(series)
	require.Equal(t, 1, len(got.Points.Timestamps))
	require.Equal(t, 0, len(got.Points.StartTimes))
}
