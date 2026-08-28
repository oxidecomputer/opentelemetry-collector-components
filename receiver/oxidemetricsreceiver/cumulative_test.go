package oxidemetricsreceiver

import (
	"testing"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
)

var (
	t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 = t0.Add(5 * time.Second)
	t2 = t0.Add(10 * time.Second)
	t3 = t0.Add(15 * time.Second)

	epoch1 = time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	epoch2 = time.Date(2026, 1, 1, 0, 0, 12, 0, time.UTC)
)

func TestAccumulate_Integer(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epoch1, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{
							Values: []*int{
								oxide.NewPointer(100),
								oxide.NewPointer(10),
								oxide.NewPointer(15),
							},
						},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	require.Equal(t, []int{100, 110, 125}, derefSlice(v.Values))
}

func TestAccumulate_Integer_Reset(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2, t3},
			StartTimes: []time.Time{epoch1, t0, epoch2, t2},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{
							Values: []*int{
								oxide.NewPointer(100),
								oxide.NewPointer(10),
								oxide.NewPointer(50),
								oxide.NewPointer(5),
							},
						},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	require.Equal(t, []int{100, 110, 50, 55}, derefSlice(v.Values))
}

func TestAccumulate_Double(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epoch1, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeDelta,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{
							Values: []*float64{
								oxide.NewPointer(1.5),
								oxide.NewPointer(0.5),
								oxide.NewPointer(0.25),
							},
						},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.InDeltaSlice(t, []float64{1.5, 2.0, 2.25}, derefSlice(v.Values), 1e-9)
}

func TestAccumulate_Gauge_Passthrough(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeGauge,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDouble{
							Values: []*float64{
								oxide.NewPointer(1.0),
								oxide.NewPointer(2.0),
								oxide.NewPointer(3.0),
							},
						},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDouble)
	require.Equal(t, []float64{1.0, 2.0, 3.0}, derefSlice(v.Values))
}

func TestAccumulate_SinglePoint(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0},
			StartTimes: []time.Time{epoch1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayInteger{Values: []*int{oxide.NewPointer(42)}},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayInteger)
	require.Equal(t, []int{42}, derefSlice(v.Values))
}

func TestAccumulate_Empty(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{},
	}

	got, err := accumulate(series)
	require.NoError(t, err)
	require.Empty(t, got.Points.Values)
}

func TestAccumulate_IntegerDistribution(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epoch1, t0, t1},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayIntegerDistribution{
							Values: []*oxide.Distributionint64{
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

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayIntegerDistribution)
	require.Equal(t, []uint64{5, 3, 2}, v.Values[0].Counts)
	require.Equal(t, []uint64{6, 4, 2}, v.Values[1].Counts)
	require.Equal(t, []uint64{8, 4, 3}, v.Values[2].Counts)
}

func TestAccumulate_DoubleDistribution(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1},
			StartTimes: []time.Time{epoch1, t0},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayDoubleDistribution{
							Values: []*oxide.Distributiondouble{
								{Bins: []float64{1.0, 10.0}, Counts: []uint64{10, 5, 3}},
								{Bins: []float64{1.0, 10.0}, Counts: []uint64{2, 1, 0}},
							},
						},
					},
				},
			},
		},
	}

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayDoubleDistribution)
	require.Equal(t, []uint64{10, 5, 3}, v.Values[0].Counts)
	require.Equal(t, []uint64{12, 6, 3}, v.Values[1].Counts)
}

func TestAccumulate_Distribution_Reset(t *testing.T) {
	series := oxide.Timeseries{
		Points: oxide.Points{
			Timestamps: []time.Time{t0, t1, t2},
			StartTimes: []time.Time{epoch1, t0, epoch2},
			Values: []oxide.Values{
				{
					MetricType: oxide.MetricTypeCumulative,
					Values: oxide.ValueArray{
						Value: &oxide.ValueArrayIntegerDistribution{
							Values: []*oxide.Distributionint64{
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

	got, err := accumulate(series)
	require.NoError(t, err)

	v := got.Points.Values[0].Values.Value.(*oxide.ValueArrayIntegerDistribution)
	require.Equal(t, []uint64{5, 3, 2}, v.Values[0].Counts)
	require.Equal(t, []uint64{6, 4, 2}, v.Values[1].Counts)
	require.Equal(t, []uint64{7, 0, 0}, v.Values[2].Counts) // fresh start
}

func derefSlice[T any](values []*T) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		out = append(out, *value)
	}
	return out
}
