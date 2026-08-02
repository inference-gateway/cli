package config

import "testing"

func TestVisionIsTextOnlyModel(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		model    string
		want     bool
	}{
		{"empty list", nil, "deepseek/deepseek-chat", false},
		{"empty model", []string{"deepseek"}, "", false},
		{"substring match", []string{"deepseek"}, "deepseek/deepseek-chat", true},
		{"case insensitive", []string{"DeepSeek"}, "deepseek/deepseek-chat", true},
		{"provider-qualified", []string{"ollama/qwen3"}, "ollama/qwen3:8b", true},
		{"no match", []string{"deepseek"}, "anthropic/claude-sonnet-5", false},
		{"blank entries ignored", []string{" ", ""}, "deepseek/deepseek-chat", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := VisionConfig{TextOnlyModels: tt.patterns}
			if got := v.IsTextOnlyModel(tt.model); got != tt.want {
				t.Fatalf("IsTextOnlyModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestVisionAnnotatorReady(t *testing.T) {
	tests := []struct {
		name string
		cfg  VisionAnnotatorConfig
		want bool
	}{
		{"disabled", VisionAnnotatorConfig{Enabled: false, Model: "qwen3-vl-2b"}, false},
		{"no model", VisionAnnotatorConfig{Enabled: true}, false},
		{"local ready", VisionAnnotatorConfig{Enabled: true, Engine: VisionEngineLocal, Model: "qwen3-vl-2b"}, true},
		{"gateway needs provider", VisionAnnotatorConfig{Enabled: true, Engine: VisionEngineGateway, Model: "sonnet"}, false},
		{"gateway ready", VisionAnnotatorConfig{Enabled: true, Engine: VisionEngineGateway, Model: "anthropic/claude-sonnet-5"}, true},
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
		{"bad engine", VisionConfig{Annotator: VisionAnnotatorConfig{Enabled: true, Engine: "sidecar", Model: "x"}}, true},
		{"enabled without model", VisionConfig{Annotator: VisionAnnotatorConfig{Enabled: true, Engine: VisionEngineLocal}}, true},
		{"gateway without provider", VisionConfig{Annotator: VisionAnnotatorConfig{Enabled: true, Engine: VisionEngineGateway, Model: "sonnet"}}, true},
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
