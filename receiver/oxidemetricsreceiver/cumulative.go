package oxidemetricsreceiver

import (
	"fmt"

	"github.com/oxidecomputer/oxide.go/oxide"
)

// toCumulative converts an OxQL timeseries from delta format to fully
// cumulative format. OxQL automatically converts cumulative counters to
// deltas when querying, so all returned values are deltas.
//
// To reconstruct cumulative values, we sum the deltas starting from the
// 0th value (which is the cumulative total since the original epoch).
// Counter resets are detected by comparing start_times[j] to
// timestamps[j-1]: in the normal case, start_times[j] equals the
// previous timestamp (the delta covers that interval). On a reset,
// start_times[j] is the new epoch's start time, which won't match the
// previous timestamp. In that case, the value is the raw cumulative
// value since the new epoch, and we use it as the new base.
//
// Gauge series are returned unmodified.
//
// TODO: optionally skip delta transformation of cumulative metrics in oximeter so that we don't
// have to convert back to cumulative.
func toCumulative(series oxide.Timeseries) (oxide.Timeseries, error) {
	if len(series.Points.Values) == 0 {
		return series, nil
	}
	if series.Points.Values[0].MetricType == oxide.MetricTypeGauge {
		return series, nil
	}

	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes

	for i, point := range series.Points.Values {
		switch v := point.Values.Value.(type) {
		case *oxide.ValueArrayInteger:
			if err := validateLen(len(timestamps), len(v.Values)); err != nil {
				return series, err
			}
			var cumulative int
			for j := range v.Values {
				if j == 0 {
					cumulative = v.Values[j]
				} else if startTimes[j] != timestamps[j-1] {
					// Reset: value is the raw cumulative since the new epoch.
					cumulative = v.Values[j]
				} else {
					cumulative += v.Values[j]
				}
				v.Values[j] = cumulative
			}
			series.Points.Values[i].Values.Value = v
		case *oxide.ValueArrayDouble:
			if err := validateLen(len(timestamps), len(v.Values)); err != nil {
				return series, err
			}
			var cumulative float64
			for j := range v.Values {
				if j == 0 {
					cumulative = v.Values[j]
				} else if startTimes[j] != timestamps[j-1] {
					cumulative = v.Values[j]
				} else {
					cumulative += v.Values[j]
				}
				v.Values[j] = cumulative
			}
			series.Points.Values[i].Values.Value = v
		case *oxide.ValueArrayIntegerDistribution:
			if err := validateLen(len(timestamps), len(v.Values)); err != nil {
				return series, err
			}
			if len(v.Values) == 0 {
				continue
			}
			prev := cloneDistInt(v.Values[0])
			for j := 1; j < len(v.Values); j++ {
				if startTimes[j] != timestamps[j-1] {
					prev = cloneDistInt(v.Values[j])
				} else {
					accumulateDistInt(&prev, v.Values[j])
				}
				v.Values[j] = cloneDistInt(prev)
			}
			series.Points.Values[i].Values.Value = v
		case *oxide.ValueArrayDoubleDistribution:
			if err := validateLen(len(timestamps), len(v.Values)); err != nil {
				return series, err
			}
			if len(v.Values) == 0 {
				continue
			}
			prev := cloneDistDouble(v.Values[0])
			for j := 1; j < len(v.Values); j++ {
				if startTimes[j] != timestamps[j-1] {
					prev = cloneDistDouble(v.Values[j])
				} else {
					accumulateDistDouble(&prev, v.Values[j])
				}
				v.Values[j] = cloneDistDouble(prev)
			}
			series.Points.Values[i].Values.Value = v
		default:
			return series, fmt.Errorf("toCumulative: unexpected value type %T", point.Values.Value)
		}
	}
	return series, nil
}

func validateLen(timestamps, values int) error {
	if timestamps != values {
		return fmt.Errorf(
			"invariant violated: number of timestamps %d must match number of values %d",
			timestamps, values,
		)
	}
	return nil
}

func cloneDistInt(d oxide.Distributionint64) oxide.Distributionint64 {
	counts := make([]uint64, len(d.Counts))
	copy(counts, d.Counts)
	bins := make([]int, len(d.Bins))
	copy(bins, d.Bins)
	return oxide.Distributionint64{
		Bins:   bins,
		Counts: counts,
	}
}

func cloneDistDouble(d oxide.Distributiondouble) oxide.Distributiondouble {
	counts := make([]uint64, len(d.Counts))
	copy(counts, d.Counts)
	bins := make([]float64, len(d.Bins))
	copy(bins, d.Bins)
	return oxide.Distributiondouble{
		Bins:   bins,
		Counts: counts,
	}
}

func accumulateDistInt(cumulative *oxide.Distributionint64, delta oxide.Distributionint64) {
	for i := range delta.Counts {
		if i < len(cumulative.Counts) {
			cumulative.Counts[i] += delta.Counts[i]
		}
	}
}

func accumulateDistDouble(cumulative *oxide.Distributiondouble, delta oxide.Distributiondouble) {
	for i := range delta.Counts {
		if i < len(cumulative.Counts) {
			cumulative.Counts[i] += delta.Counts[i]
		}
	}
}
