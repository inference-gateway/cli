package githubscheduler

import (
	"strings"
	"testing"
)

func TestTranslateCron(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string // substring of the expected error; empty means success
	}{
		{"hourly", "@hourly", "0 * * * *", ""},
		{"daily", "@daily", "0 0 * * *", ""},
		{"midnight", "@midnight", "0 0 * * *", ""},
		{"weekly", "@weekly", "0 0 * * 0", ""},
		{"monthly", "@monthly", "0 0 1 * *", ""},
		{"yearly", "@yearly", "0 0 1 1 *", ""},
		{"annually", "@annually", "0 0 1 1 *", ""},
		{"descriptor case-insensitive", "@Daily", "0 0 * * *", ""},
		{"every 10m", "@every 10m", "*/10 * * * *", ""},
		{"every 5m", "@every 5m", "*/5 * * * *", ""},
		{"every 30m", "@every 30m", "*/30 * * * *", ""},
		{"every 1h", "@every 1h", "0 * * * *", ""},
		{"every 60m", "@every 60m", "0 * * * *", ""},
		{"every 2h", "@every 2h", "0 */2 * * *", ""},
		{"every 6h", "@every 6h", "0 */6 * * *", ""},
		{"every 24h", "@every 24h", "0 0 * * *", ""},
		{"every 1m too small", "@every 1m", "", "GitHub Actions cron"},
		{"every 90s not whole minutes", "@every 90s", "", "GitHub Actions cron"},
		{"every 7m does not divide 60", "@every 7m", "", "GitHub Actions cron"},
		{"every 5h does not divide 24", "@every 5h", "", "GitHub Actions cron"},
		{"every 90m mixed", "@every 90m", "", "GitHub Actions cron"},
		{"every 48h too big", "@every 48h", "", "GitHub Actions cron"},
		{"every garbage", "@every soon", "", "invalid @every duration"},
		{"unknown descriptor", "@reboot", "", "not supported"},
		{"plain 5-field", "0 8 * * *", "0 8 * * *", ""},
		{"5-field trimmed", "  30 6 * * 1  ", "30 6 * * 1", ""},
		{"5-field every minute", "* * * * *", "", "fires every minute"},
		{"5-field sub-5min step", "*/2 * * * *", "", "fires every 2 minute"},
		{"5-field 5min step ok", "*/5 * * * *", "*/5 * * * *", ""},
		{"invalid expression", "not a cron", "", "invalid cron expression"},
		{"six fields", "0 0 8 * * *", "", "invalid cron expression"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TranslateCron(tt.in)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("TranslateCron(%q) = %q, want error containing %q", tt.in, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("TranslateCron(%q) error = %v, want substring %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("TranslateCron(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("TranslateCron(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
