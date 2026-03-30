package oxidemetricsreceiver

import (
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
)

// dedup removes samples that share the same millisecond-truncated timestamp
// within a timeseries, keeping only the last nanosecond-resolution sample
// in each millisecond. This is necessary because Prometheus uses millisecond
// timestamp precision, and will reject a remote write batch if it contains
// two samples for the same series at the same millisecond with different values.
//
// The choice of "last wins" is deterministic: OxQL returns timestamps sorted
// in ascending order, so both overlapping scrapes will see the same nanosecond
// timestamps and pick the same winner.
func dedup(series oxide.Timeseries) oxide.Timeseries {
	n := len(series.Points.Timestamps)
	if n <= 1 {
		return series
	}

	hasStartTimes := len(series.Points.StartTimes) == n

	// Find which indices to keep: for each run of timestamps that share
	// the same millisecond, keep only the last one.
	keep := make([]bool, n)
	for i := 0; i < n-1; i++ {
		if series.Points.Timestamps[i].Truncate(time.Millisecond) !=
			series.Points.Timestamps[i+1].Truncate(time.Millisecond) {
			keep[i] = true
		}
	}
	keep[n-1] = true // always keep the last

	// Count how many we're keeping.
	count := 0
	for _, k := range keep {
		if k {
			count++
		}
	}
	if count == n {
		return series // nothing to dedup
	}

	// Compact timestamps and start_times.
	newTimestamps := make([]time.Time, 0, count)
	var newStartTimes []time.Time
	if hasStartTimes {
		newStartTimes = make([]time.Time, 0, count)
	}
	for i, k := range keep {
		if k {
			newTimestamps = append(newTimestamps, series.Points.Timestamps[i])
			if hasStartTimes {
				newStartTimes = append(newStartTimes, series.Points.StartTimes[i])
			}
		}
	}
	series.Points.Timestamps = newTimestamps
	series.Points.StartTimes = newStartTimes

	// Compact each value array.
	for idx, point := range series.Points.Values {
		switch v := point.Values.Value.(type) {
		case *oxide.ValueArrayInteger:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		case *oxide.ValueArrayDouble:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		case *oxide.ValueArrayBoolean:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		case *oxide.ValueArrayString:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		case *oxide.ValueArrayIntegerDistribution:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		case *oxide.ValueArrayDoubleDistribution:
			v.Values = compactSlice(v.Values, keep)
			series.Points.Values[idx].Values.Value = v
		}
	}

	return series
}

func compactSlice[T any](s []T, keep []bool) []T {
	result := make([]T, 0, len(s))
	for i, k := range keep {
		if k && i < len(s) {
			result = append(result, s[i])
		}
	}
	return result
}
