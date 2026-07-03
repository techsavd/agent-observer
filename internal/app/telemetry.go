package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/techsavd/agent-observer/core/schema"
)

const telemetrySchemaVersion = "v1"

type telemetryCounts struct {
	Tasks    int `json:"tasks"`
	Batches  int `json:"batches"`
	Warnings int `json:"warnings"`
}

type telemetryEvent struct {
	SchemaVersion string          `json:"schema_version"`
	EventType     string          `json:"event_type"`
	AppVersion    string          `json:"app_version"`
	Commit        string          `json:"commit"`
	GOOS          string          `json:"goos"`
	GOARCH        string          `json:"goarch"`
	Command       string          `json:"command"`
	Counts        telemetryCounts `json:"counts"`
	DurationMS    int64           `json:"duration_ms"`
	WarningCount  int             `json:"warning_count"`
	ErrorCategory string          `json:"error_category,omitempty"`
	ObservedAt    time.Time       `json:"observed_at"`
}

type telemetryClient interface {
	Track(context.Context, telemetryEvent) error
}

type noopTelemetryClient struct{}

func (noopTelemetryClient) Track(context.Context, telemetryEvent) error { return nil }

type httpTelemetryClient struct {
	endpoint string
	client   *http.Client
}

func newTelemetryClient(opts options) telemetryClient {
	if opts.telemetry != "on" || opts.telemetryURL == "" {
		return noopTelemetryClient{}
	}
	return httpTelemetryClient{
		endpoint: opts.telemetryURL,
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

func (c httpTelemetryClient) Track(ctx context.Context, event telemetryEvent) error {
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("telemetry endpoint returned %s", resp.Status)
	}
	return nil
}

func buildTelemetryEvent(eventType string, opts options, world schema.WorldSnapshot, errorCategory string) telemetryEvent {
	return telemetryEvent{
		SchemaVersion: telemetrySchemaVersion,
		EventType:     eventType,
		AppVersion:    Version,
		Commit:        Commit,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Command:       string(opts.command),
		Counts: telemetryCounts{
			Tasks:    len(world.Tasks),
			Batches:  len(world.Batches),
			Warnings: len(world.Warnings),
		},
		DurationMS:    world.Stats.LastDuration.Milliseconds(),
		WarningCount:  len(world.Warnings),
		ErrorCategory: errorCategory,
		ObservedAt:    time.Now().UTC(),
	}
}

func trackTelemetry(ctx context.Context, logger *slog.Logger, client telemetryClient, event telemetryEvent) {
	if client == nil {
		return
	}
	if err := client.Track(ctx, event); err != nil {
		logger.Debug("telemetry send failed", slog.String("event_type", event.EventType), slog.String("error", err.Error()))
	}
}

func errorCategory(err error) string {
	if err == nil {
		return ""
	}
	return "runtime_error"
}
