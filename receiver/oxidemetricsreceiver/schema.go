package oxidemetricsreceiver

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/BurntSushi/toml"
)

//go:embed schema/*.toml
var schemaFS embed.FS

var metricMetadata map[string]oximeterMetadata

type oximeterMetadata struct {
	Target oximeterTarget
	Metric oximeterMetric
}

type oximeterSchema struct {
	Target  oximeterTarget   `toml:"target"`
	Metrics []oximeterMetric `toml:"metrics"`
}

type oximeterTarget struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

type oximeterMetric struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Units       string `toml:"units"`
}

// oximeterUnitsToOtel maps oximeter unit strings to OpenTelemetry UCUM unit strings.
// See https://ucum.org/ucum and https://opentelemetry.io/docs/specs/semconv/general/metrics/.
var oximeterUnitsToOtel = map[string]string{
	"nanoseconds":     "ns",
	"seconds":         "s",
	"bytes":           "By",
	"degrees_celsius": "Cel",
	"amps":            "A",
	"volts":           "V",
	"watts":           "W",
	"rpm":             "{rpm}",
	"count":           "{count}",
	"none":            "",
}

// formatUnits converts an oximeter unit string to an OpenTelemetry UCUM unit string.
func formatUnits(unit string) string {
	if otel, ok := oximeterUnitsToOtel[unit]; ok {
		return otel
	}
	return unit
}

func init() {
	metricMetadata = make(map[string]oximeterMetadata)

	dirs, err := fs.ReadDir(schemaFS, "schema")
	if err != nil {
		panic(err)
	}
	for _, dir := range dirs {
		var schema oximeterSchema
		if _, err := toml.DecodeFS(schemaFS, "schema/"+dir.Name(), &schema); err != nil {
			panic(err)
		}
		for _, metric := range schema.Metrics {
			key := fmt.Sprintf("%s:%s", schema.Target.Name, metric.Name)
			if _, ok := metricMetadata[key]; ok {
				panic(fmt.Sprintf("duplicate key %s in oximeter metadata", key))
			}
			metricMetadata[key] = oximeterMetadata{
				Target: schema.Target,
				Metric: metric,
			}
		}
	}
}
