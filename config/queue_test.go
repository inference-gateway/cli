package config

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidate_QueueType(t *testing.T) {
	tests := []struct {
		name    string
		qType   QueueType
		wantErr bool
	}{
		{"default memory", QueueTypeMemory, false},
		{"empty falls back to memory", "", false},
		{"sqlite", QueueTypeSQLite, false},
		{"postgres", QueueTypePostgres, false},
		{"redis", QueueTypeRedis, false},
		{"jsonl is a storage type, not a queue backend", QueueType(StorageTypeJsonl), true},
		{"d1 is not a queue backend", QueueType(StorageTypeD1), true},
		{"a typo is rejected", QueueType("memmory"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Queue.Type = tt.qType
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() with queue.type %q returned err %v, wantErr %v", tt.qType, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "queue.type") {
				t.Errorf("Validate() error %q should name queue.type", err.Error())
			}
		})
	}
}

func TestConfigValidate_QueuePollInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantErr  bool
	}{
		{"the default parses", DefaultQueuePollInterval, false},
		{"shorter intervals are fine", "200ms", false},
		{"empty defers to the resolve default", "", false},
		{"not a duration", "soon", true},
		{"negative", "-1s", true},
		{"zero", "0s", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Queue.PollInterval = tt.interval
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() with queue.poll_interval %q returned err %v, wantErr %v", tt.interval, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "queue.poll_interval") {
				t.Errorf("Validate() error %q should name queue.poll_interval", err.Error())
			}
		})
	}
}

func TestQueueConfig_ResolvePollInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		want     time.Duration
	}{
		{"empty default", "", 2 * time.Second},
		{"configured value", "5s", 5 * time.Second},
		{"invalid falls back", "soon", 2 * time.Second},
		{"zero falls back", "0s", 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (QueueConfig{PollInterval: tt.interval}).ResolvePollInterval(); got != tt.want {
				t.Errorf("ResolvePollInterval(%q) = %v, want %v", tt.interval, got, tt.want)
			}
		})
	}
}
