package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudLoggingHandler(t *testing.T) {
	t.Run("Cloud Logging 向けログ属性変換", func(t *testing.T) {
		cases := []struct {
			name         string
			log          func(l *slog.Logger)
			wantSeverity string
		}{
			{
				name:         "Error レベルで出力すると、severity が ERROR になる",
				log:          func(l *slog.Logger) { l.Error("boom") },
				wantSeverity: "ERROR",
			},
			{
				name:         "Warn レベルで出力すると、severity が WARNING になる",
				log:          func(l *slog.Logger) { l.Warn("boom") },
				wantSeverity: "WARNING",
			},
			{
				name:         "Info レベルで出力すると、severity が INFO になる",
				log:          func(l *slog.Logger) { l.Info("boom") },
				wantSeverity: "INFO",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				logger := slog.New(newCloudLoggingHandler(&buf))

				tc.log(logger)

				var record map[string]any
				require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
				assert.Equal(t, tc.wantSeverity, record["severity"])
				assert.Equal(t, "boom", record["message"])
			})
		}
	})
}
