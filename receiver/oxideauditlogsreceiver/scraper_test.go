package oxideauditlogsreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oxidecomputer/oxide.go/oxide"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/scraper/scrapererror"
)

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg:  Config{InitialLookback: 24 * time.Hour},
		},
		{
			name: "empty lookback is valid",
			cfg:  Config{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAddLogRecord(t *testing.T) {
	now := time.Now()
	startTime := now.Add(-time.Second)
	completedTime := now

	entry := oxide.AuditLogEntry{
		Id:            "entry-001",
		OperationId:   "project_create",
		RequestId:     "req-123",
		RequestUri:    "/v1/projects",
		SourceIp:      "192.168.1.1",
		TimeStarted:   &startTime,
		TimeCompleted: &completedTime,
		Actor: oxide.AuditLogEntryActor{
			Value: &oxide.AuditLogEntryActorSiloUser{SiloUserId: "user-001"},
		},
		Result: oxide.AuditLogEntryResult{
			Value: &oxide.AuditLogEntryResultSuccess{
				HttpStatusCode: oxide.NewPointer(200),
			},
		},
	}

	logs := plog.NewLogs()
	observedTime := now.Add(time.Minute)
	require.NoError(t, addLogRecord(logs, entry, observedTime))

	require.Equal(t, 1, logs.ResourceLogs().Len())
	resource := logs.ResourceLogs().At(0)

	// Check resource attributes.
	serviceName, ok := resource.Resource().Attributes().Get("service.name")
	require.True(t, ok)
	require.Equal(t, "oxide", serviceName.Str())

	// Check log record.
	require.Equal(t, 1, resource.ScopeLogs().Len())
	require.Equal(t, 1, resource.ScopeLogs().At(0).LogRecords().Len())
	log := resource.ScopeLogs().At(0).LogRecords().At(0)

	require.Equal(t,
		pcommon.NewTimestampFromTime(startTime),
		log.Timestamp(),
	)
	require.Equal(t,
		pcommon.NewTimestampFromTime(observedTime),
		log.ObservedTimestamp(),
	)

	// Check body.
	require.Equal(t, pcommon.ValueTypeMap, log.Body().Type())
	raw, err := json.Marshal(entry)
	require.NoError(t, err)
	var want map[string]any
	require.NoError(t, json.Unmarshal(raw, &want))
	require.Equal(t, want, log.Body().Map().AsRaw())
}

func TestCursor(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)
	t3 := time.Date(2025, 1, 1, 0, 0, 2, 0, time.UTC)

	s := &auditLogScraper{
		cursor: cursor{
			TimeCompleted: &t1,
			ID:            "id-0",
		},
	}

	makeActor := func() oxide.AuditLogEntryActor {
		return oxide.AuditLogEntryActor{
			Value: &oxide.AuditLogEntryActorUnauthenticated{},
		}
	}
	makeResult := func() oxide.AuditLogEntryResult {
		return oxide.AuditLogEntryResult{
			Value: &oxide.AuditLogEntryResultUnknown{},
		}
	}

	entries := []oxide.AuditLogEntry{
		{
			Id:            "id-0",
			TimeStarted:   &t1,
			TimeCompleted: &t1,
			Actor:         makeActor(),
			Result:        makeResult(),
		},
		{
			Id:            "id-1",
			TimeStarted:   &t2,
			TimeCompleted: &t2,
			Actor:         makeActor(),
			Result:        makeResult(),
		},
		{
			Id:            "id-2",
			TimeStarted:   &t3,
			TimeCompleted: &t3,
			Actor:         makeActor(),
			Result:        makeResult(),
		},
	}

	logs := plog.NewLogs()
	now := time.Now()
	for _, entry := range entries {
		if entry.Id == s.cursor.ID {
			continue
		}
		require.NoError(t, addLogRecord(logs, entry, now))
		if entry.TimeCompleted != nil {
			s.cursor = cursor{
				TimeCompleted: entry.TimeCompleted,
				ID:            entry.Id,
			}
		}
	}

	// Check that duplicate log was skipped.
	require.Equal(t, 2, logs.LogRecordCount())

	// Cursor should point to the last entry.
	require.Equal(t, "id-2", s.cursor.ID)
	require.Equal(t, t3, *s.cursor.TimeCompleted)
}

func makeEntry(id string, tc *time.Time) oxide.AuditLogEntry {
	return oxide.AuditLogEntry{
		Id:            id,
		TimeStarted:   tc,
		TimeCompleted: tc,
		Actor: oxide.AuditLogEntryActor{
			Value: &oxide.AuditLogEntryActorUnauthenticated{},
		},
		Result: oxide.AuditLogEntryResult{
			Value: &oxide.AuditLogEntryResultUnknown{},
		},
	}
}

// mockAuditLogClient returns pages in sequence, allowing injection of errors.
type mockAuditLogClient struct {
	pages []mockPage
	calls int
}

type mockPage struct {
	result *oxide.AuditLogEntryResultsPage
	err    error
}

func (m *mockAuditLogClient) AuditLogList(
	_ context.Context,
	_ oxide.AuditLogListParams,
) (*oxide.AuditLogEntryResultsPage, error) {
	if m.calls >= len(m.pages) {
		return nil, errors.New("no more pages configured")
	}
	page := m.pages[m.calls]
	m.calls++
	return page.result, page.err
}

func TestScrapePartialResults(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)

	client := &mockAuditLogClient{
		pages: []mockPage{
			{
				result: &oxide.AuditLogEntryResultsPage{
					Items:    []oxide.AuditLogEntry{makeEntry("id-1", &t1)},
					NextPage: "page2",
				},
			},
			{
				err: errors.New("boom"),
			},
		},
	}

	s := newAuditLogScraper(
		&Config{InitialLookback: time.Hour},
		componenttest.NewNopTelemetrySettings(),
		client,
	)
	require.NoError(t, s.Start(context.Background(), nil))
	s.cursor = cursor{TimeCompleted: &t2}

	logs, err := s.Scrape(context.Background())

	// Should return a PartialScrapeError since page 1 succeeded but page 2 failed.
	require.Error(t, err)
	require.True(t, scrapererror.IsPartialScrapeError(err))

	// The one entry from the first page should still be present.
	require.Equal(t, 1, logs.LogRecordCount())

	// Cursor should point to the last successful entry.
	require.Equal(t, "id-1", s.cursor.ID)
	require.Equal(t, t1, *s.cursor.TimeCompleted)
}

func TestCursorPersistence(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC)

	cursorPath := filepath.Join(t.TempDir(), "cursor.json")

	client := &mockAuditLogClient{
		pages: []mockPage{
			{
				result: &oxide.AuditLogEntryResultsPage{
					Items: []oxide.AuditLogEntry{
						makeEntry("id-1", &t1),
						makeEntry("id-2", &t2),
					},
				},
			},
		},
	}

	cfg := &Config{
		InitialLookback: time.Hour,
		CursorPath:      cursorPath,
	}

	// First scraper: cursor file doesn't exist yet, should fall back to InitialLookback.
	s := newAuditLogScraper(cfg, componenttest.NewNopTelemetrySettings(), client)
	require.NoError(t, s.Start(context.Background(), nil))
	require.Empty(t, s.cursor.ID)
	require.WithinDuration(t, time.Now().Add(-time.Hour), *s.cursor.TimeCompleted, 5*time.Second)

	_, err := s.Scrape(context.Background())
	require.NoError(t, err)

	// Verify cursor file exists and has correct content.
	data, err := os.ReadFile(cursorPath)
	require.NoError(t, err)
	var saved cursor
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, "id-2", saved.ID)
	require.Equal(t, t2, *saved.TimeCompleted)

	// Second scraper: verify it loads cursor from file instead of using InitialLookback.
	s2 := newAuditLogScraper(cfg, componenttest.NewNopTelemetrySettings(), client)
	require.NoError(t, s2.Start(context.Background(), nil))
	require.Equal(t, "id-2", s2.cursor.ID)
	require.Equal(t, t2, *s2.cursor.TimeCompleted)
}
