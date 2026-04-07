package oxidemetricsreceiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricMetadataPopulated(t *testing.T) {
	require.NotEmpty(t, metricMetadata)

	// Spot-check a known metric.
	m, ok := metricMetadata["virtual_machine:vcpu_usage"]
	require.True(t, ok, "expected virtual_machine:vcpu_usage in metricMetadata")
	assert.Equal(t, "virtual_machine", m.Target.Name)
	assert.Equal(t, "vcpu_usage", m.Metric.Name)
	assert.Equal(t, "nanoseconds", m.Metric.Units)
	assert.NotEmpty(t, m.Metric.Description)
	assert.NotEmpty(t, m.Target.Description)
}
