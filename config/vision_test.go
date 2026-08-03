package config

import "testing"

func TestVisionAnnotatorReady(t *testing.T) {
	tests := []struct {
		name string
		cfg  VisionAnnotatorConfig
		want bool
	}{
		{"disabled", VisionAnnotatorConfig{Enabled: false, Model: "qwen3-vl-2b"}, false},
		{"no model", VisionAnnotatorConfig{Enabled: true}, false},
		{"local ready", VisionAnnotatorConfig{Enabled: true, Model: "qwen3-vl-2b"}, true},
		{"gateway ready", VisionAnnotatorConfig{Enabled: true, Model: "anthropic/claude-sonnet-5"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := VisionConfig{Annotator: tt.cfg}
			if got := v.AnnotatorReady(); got != tt.want {
				t.Fatalf("AnnotatorReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVisionValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     VisionConfig
		wantErr bool
	}{
		{"zero value", VisionConfig{}, false},
		{"default config", DefaultConfig().Vision, false},
		{"enabled without model", VisionConfig{Annotator: VisionAnnotatorConfig{Enabled: true}}, true},
		{"bad source type", VisionConfig{Sources: map[string]VisionSourceConfig{"cam": {Type: "rtsp", Path: "/x"}}}, true},
		{"source without path", VisionConfig{Sources: map[string]VisionSourceConfig{"cam": {Type: "directory"}}}, true},
		{"bad max_age", VisionConfig{Sources: map[string]VisionSourceConfig{"cam": {Type: "directory", Path: "/x", Retention: VisionRetentionConfig{MaxAge: "yesterday"}}}}, true},
		{"valid directory source", VisionConfig{Sources: map[string]VisionSourceConfig{"cam": {Type: "directory", Path: "/x", Retention: VisionRetentionConfig{MaxFiles: 10, MaxAge: "24h"}}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
