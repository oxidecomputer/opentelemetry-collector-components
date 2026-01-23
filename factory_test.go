package oxidereceiver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
)

func TestMakeOxideClientOptions(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	for _, tc := range []struct {
		name               string
		insecureSkipVerify bool
		canConnectToTLS    bool
	}{
		{
			name:               "skip verify",
			insecureSkipVerify: true,
			canConnectToTLS:    true,
		},
		{
			name:               "verify",
			insecureSkipVerify: false,
			canConnectToTLS:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Host:               ts.URL,
				Token:              "test-token",
				InsecureSkipVerify: tc.insecureSkipVerify,
			}

			opts := makeOxideClientOptions(cfg)

			client, err := oxide.NewClient(opts...)
			require.NoError(t, err)

			resp, err := client.MakeRequest(context.Background(), oxide.Request{
				Method: http.MethodGet,
				Path:   "/",
			})

			if tc.canConnectToTLS {
				require.NoError(t, err)
				_ = resp.Body.Close()
			} else {
				require.Error(
					t,
					err,
					"Should not be able to connect with InsecureSkipVerify=false to self-signed cert",
				)
				require.Contains(
					t,
					err.Error(),
					"certificate",
					"Error should be about certificate verification",
				)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)

	require.Equal(t, []string{".*"}, cfg.MetricPatterns)
	require.Equal(t, 16, cfg.ScrapeConcurrency)
	require.Equal(t, "5m", cfg.QueryLookback)
	require.False(t, cfg.InsecureSkipVerify, "InsecureSkipVerify should default to false")
}
