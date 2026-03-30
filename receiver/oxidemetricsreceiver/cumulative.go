package oxidemetricsreceiver

import (
	"fmt"

	"github.com/oxidecomputer/oxide.go/oxide"
)

// accumulate converts an OxQL timeseries from delta format to cumulative.
//
// TODO: optionally skip delta transformation of cumulative metrics in oximeter so that we don't
// have to convert back to cumulative.
func accumulate(series oxide.Timeseries) (oxide.Timeseries, error) {
	if len(series.Points.Values) == 0 {
		return series, nil
	}
	if series.Points.Values[0].MetricType == oxide.MetricTypeGauge {
		return series, nil
	}

	timestamps := series.Points.Timestamps
	startTimes := series.Points.StartTimes

	for idx, pointValue := range series.Points.Values {
		switch value := pointValue.Values.Value.(type) {
		case *oxide.ValueArrayInteger:
			var accumulator int
			for valueIdx := range value.Values {
				if value.Values[valueIdx] == nil {
					continue
				}
				if valueIdx == 0 {
					// If we're considering the 0th value, set the accumulator to the current value.
					accumulator = *value.Values[valueIdx]
				} else if startTimes[valueIdx] != timestamps[valueIdx-1] {
					// If the series has reset, set the accumulator to the current value.
					accumulator = *value.Values[valueIdx]
				} else {
					// Increment the accumulator.
					accumulator += *value.Values[valueIdx]
				}
				*value.Values[valueIdx] = accumulator
			}
			series.Points.Values[idx].Values.Value = value
		case *oxide.ValueArrayDouble:
			var accumulator float64
			for valueIdx := range value.Values {
				if value.Values[valueIdx] == nil {
					continue
				}
				if valueIdx == 0 {
					accumulator = *value.Values[valueIdx]
				} else if startTimes[valueIdx] != timestamps[valueIdx-1] {
					accumulator = *value.Values[valueIdx]
				} else {
					accumulator += *value.Values[valueIdx]
				}
				*value.Values[valueIdx] = accumulator
			}
			series.Points.Values[idx].Values.Value = value
		case *oxide.ValueArrayIntegerDistribution:
			if len(value.Values) == 0 {
				continue
			}
			var accumulator *oxide.Distributionint64
			for valueIdx := range value.Values {
				if value.Values[valueIdx] == nil {
					continue
				}
				if valueIdx == 0 || accumulator == nil {
					accumulator = cloneDistInt(value.Values[valueIdx])
				} else if startTimes[valueIdx] != timestamps[valueIdx-1] {
					accumulator = cloneDistInt(value.Values[valueIdx])
				} else {
					accumulateDistInt(accumulator, *value.Values[valueIdx])
				}
				value.Values[valueIdx] = cloneDistInt(accumulator)
			}
			series.Points.Values[idx].Values.Value = value
		case *oxide.ValueArrayDoubleDistribution:
			if len(value.Values) == 0 {
				continue
			}
			var accumulator *oxide.Distributiondouble
			for valueIdx := range value.Values {
				if value.Values[valueIdx] == nil {
					continue
				}
				if valueIdx == 0 || accumulator == nil {
					accumulator = cloneDistDouble(value.Values[valueIdx])
				} else if startTimes[valueIdx] != timestamps[valueIdx-1] {
					accumulator = cloneDistDouble(value.Values[valueIdx])
				} else {
					accumulateDistDouble(accumulator, *value.Values[valueIdx])
				}
				value.Values[valueIdx] = cloneDistDouble(accumulator)
			}
			series.Points.Values[idx].Values.Value = value
		default:
			return series, fmt.Errorf("unexpected value type %T", pointValue.Values.Value)
		}
	}
	return series, nil
}

func cloneDistInt(d *oxide.Distributionint64) *oxide.Distributionint64 {
	counts := make([]uint64, len(d.Counts))
	bins := make([]int, len(d.Bins))
	copy(counts, d.Counts)
	copy(bins, d.Bins)
	return &oxide.Distributionint64{
		Bins:   bins,
		Counts: counts,
	}
}

func cloneDistDouble(d *oxide.Distributiondouble) *oxide.Distributiondouble {
	counts := make([]uint64, len(d.Counts))
	bins := make([]float64, len(d.Bins))
	copy(counts, d.Counts)
	copy(bins, d.Bins)
	return &oxide.Distributiondouble{
		Bins:   bins,
		Counts: counts,
	}
}

func accumulateDistInt(cumulative *oxide.Distributionint64, delta oxide.Distributionint64) {
	for binIdx := range delta.Counts {
		if binIdx < len(cumulative.Counts) {
			cumulative.Counts[binIdx] += delta.Counts[binIdx]
		}
	}
}

func accumulateDistDouble(cumulative *oxide.Distributiondouble, delta oxide.Distributiondouble) {
	for binIdx := range delta.Counts {
		if binIdx < len(cumulative.Counts) {
			cumulative.Counts[binIdx] += delta.Counts[binIdx]
		}
	}
}
