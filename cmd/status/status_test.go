package status

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
)

func unreachableGateway(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &config.Config{Gateway: config.GatewayConfig{URL: "http://127.0.0.1:1"}}
}

func TestStatusStructuredFormatsReportUnreachableGateway(t *testing.T) {
	tests := []struct {
		format string
		decode func([]byte, any) error
	}{
		{"json", json.Unmarshal},
		{"yaml", yaml.Unmarshal},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			cfg := unreachableGateway(t)
			var out bytes.Buffer
			if err := runStatus(context.Background(), &out, cfg, tt.format); err != nil {
				t.Fatalf("runStatus: %v", err)
			}

			var got gatewayStatus
			if err := tt.decode(out.Bytes(), &got); err != nil {
				t.Fatalf("output is not valid %s: %v\n%s", tt.format, err, out.String())
			}
			if got.URL != cfg.Gateway.URL || got.Status != "unreachable" || got.Version != "unknown" || got.Error == "" {
				t.Errorf("unexpected status %+v", got)
			}
		})
	}
}

func TestStatusTextReportsUnreachableGateway(t *testing.T) {
	var out bytes.Buffer
	if err := runStatus(context.Background(), &out, unreachableGateway(t), "text"); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	for _, want := range []string{"Checking inference gateway status...", "Gateway Status: Unreachable (", "Models: Unable to connect"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusRejectsUnknownFormatBeforeProbing(t *testing.T) {
	var out bytes.Buffer
	err := runStatus(context.Background(), &out, unreachableGateway(t), "xml")
	if err == nil || !strings.Contains(err.Error(), `unknown output format "xml"`) {
		t.Fatalf("runStatus(xml) = %v, want an unknown-format error", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for a rejected format, got %q", out.String())
	}
}
