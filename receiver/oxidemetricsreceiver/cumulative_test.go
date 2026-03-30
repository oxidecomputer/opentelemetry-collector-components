package oxidemetricsreceiver

import (
	"testing"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
)

var (
	// Timestamps 5 seconds apart, simulating oximeter collection.
	t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(5 * time.Second)
	t2 = t0.Add(10 * time.Second)
	t3 = t0.Add(15 * time.Second)

	// Original epoch start (way in the past).
	epochA = time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	// A new epoch start after a reset.
	epochB = time.Date(2026, 1, 1, 0, 0, 12, 0, time.UTC)
)

func TestToCumulative_Integer(t *testing.T) {
	// OxQL returns all deltas. The 0th value is the cumulative total since
	// the epoch start, expressed as a delta. Subsequent values are deltas
	// over the previous interval.
	// start_times[j] == timestamps[j-1] means same epoch.
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epochA, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []int{100, 10, 15}},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	require.Equal(t, []int{100, 110, 125}, v.Values)
}

func TestToCumulative_Integer_Reset(t *testing.T) {
	// After t1, the counter resets. At t2, start_time is epochB (not t1),
	// signaling a new epoch. The value at t2 is the raw cumulative since
	// epochB.
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2, t3},
			StartTimes: []time.Time{epochA, t0, epochB, t2},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []int{100, 10, 50, 5}},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	// 100, 100+10=110, then reset: 50, 50+5=55
	require.Equal(t, []int{100, 110, 50, 55}, v.Values)
}

func TestToCumulative_Double(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epochA, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeDelta,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{Values: []float64{1.5, 0.5, 0.25}},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.InDeltaSlice(t, []float64{1.5, 2.0, 2.25}, v.Values, 1e-9)
}

func TestToCumulative_Gauge_Passthrough(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
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

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.Equal(t, []float64{1.0, 2.0, 3.0}, v.Values)
}

func TestToCumulative_SinglePoint(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0},
			StartTimes: []time.Time{epochA},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []int{42}},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	require.Equal(t, []int{42}, v.Values)
}

func TestToCumulative_Empty(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)
	require.Empty(t, got.Points.Values)
}

func TestToCumulative_IntegerDistribution(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epochA, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayIntegerDistribution{
							Values: []oxide.Distributionint64{
								{Bins: []int{10, 100}, Counts: []uint64{5, 3, 2}},
								{Bins: []int{10, 100}, Counts: []uint64{1, 1, 0}},
								{Bins: []int{10, 100}, Counts: []uint64{2, 0, 1}},
							},
						},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayIntegerDistribution)
	require.Equal(t, []uint64{5, 3, 2}, v.Values[0].Counts)
	require.Equal(t, []uint64{6, 4, 2}, v.Values[1].Counts)
	require.Equal(t, []uint64{8, 4, 3}, v.Values[2].Counts)
}

func TestToCumulative_DoubleDistribution(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1},
			StartTimes: []time.Time{epochA, t0},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDoubleDistribution{
							Values: []oxide.Distributiondouble{
								{Bins: []float64{1.0, 10.0}, Counts: []uint64{10, 5, 3}},
								{Bins: []float64{1.0, 10.0}, Counts: []uint64{2, 1, 0}},
							},
						},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDoubleDistribution)
	require.Equal(t, []uint64{10, 5, 3}, v.Values[0].Counts)
	require.Equal(t, []uint64{12, 6, 3}, v.Values[1].Counts)
}

func TestToCumulative_Distribution_Reset(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epochA, t0, epochB},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayIntegerDistribution{
							Values: []oxide.Distributionint64{
								{Bins: []int{10, 100}, Counts: []uint64{5, 3, 2}},
								{Bins: []int{10, 100}, Counts: []uint64{1, 1, 0}},
								{Bins: []int{10, 100}, Counts: []uint64{7, 0, 0}}, // reset
							},
						},
					},
				},
			},
		},
	}

	got, err := toCumulative(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayIntegerDistribution)
	require.Equal(t, []uint64{5, 3, 2}, v.Values[0].Counts)
	require.Equal(t, []uint64{6, 4, 2}, v.Values[1].Counts)
	require.Equal(t, []uint64{7, 0, 0}, v.Values[2].Counts) // fresh start
}

func TestToCumulative_LengthMismatch(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1},
			StartTimes: []time.Time{epochA, t0},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []int{100, 10, 15}},
					},
				},
			},
		},
	}

	_, err := toCumulative(series)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invariant violated")
}
