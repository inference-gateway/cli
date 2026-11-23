package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestDefaultComposerConfigTriggers(t *testing.T) {
	tests := []struct {
		name    string
		context string
		trigger string
		want    []string
	}{
		{"chat at opens files with agents after them", ComposerContextChat, "@", []string{ComposerSourceFiles, ComposerSourceAgents}},
		{"chat hash opens issues", ComposerContextChat, "#", []string{ComposerSourceIssues}},
		{"room at opens agents with files after them", ComposerContextRoom, "@", []string{ComposerSourceAgents, ComposerSourceFiles}},
		{"room hash opens issues", ComposerContextRoom, "#", []string{ComposerSourceIssues}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultComposerConfig().Triggers[tt.context][tt.trigger]
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Triggers[%q][%q] = %v, want %v", tt.context, tt.trigger, got, tt.want)
			}
		})
	}
}

func TestDefaultConfigCarriesComposer(t *testing.T) {
	cfg := DefaultConfig()
	if !reflect.DeepEqual(cfg.Composer.Triggers, DefaultComposerConfig().Triggers) {
		t.Errorf("DefaultConfig triggers differ from the composer defaults: %v", cfg.Composer.Triggers)
	}
	if cfg.Composer.StatusBar != GetDefaultStatusBarConfig() {
		t.Errorf("DefaultConfig status bar differs from the status bar defaults: %+v", cfg.Composer.StatusBar)
	}
}

func TestComposerValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ComposerConfig)
		wantErr string
	}{
		{"defaults validate", func(*ComposerConfig) {}, ""},
		{"reserved slash trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["/"] = []string{ComposerSourceFiles}
		}, `triggers.chat."/"`},
		{"reserved single bang trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["!"] = []string{ComposerSourceAgents}
		}, `triggers.chat."!"`},
		{"reserved double bang trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextRoom]["!!"] = []string{ComposerSourceIssues}
		}, `triggers.room."!!"`},
		{"ascii special character trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["^"] = []string{ComposerSourceIssues}
		}, ""},
		{"non-ascii special character trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["§"] = []string{ComposerSourceFiles}
		}, ""},
		{"letter trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["a"] = []string{ComposerSourceFiles}
		}, `triggers.chat."a"`},
		{"digit trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["1"] = []string{ComposerSourceFiles}
		}, `triggers.chat."1"`},
		{"multi-character trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["@@"] = []string{ComposerSourceFiles}
		}, `triggers.chat."@@"`},
		{"empty trigger", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat][""] = []string{ComposerSourceFiles}
		}, `triggers.chat.""`},
		{"unknown context", func(c *ComposerConfig) {
			c.Triggers["meeting"] = map[string][]string{"@": {ComposerSourceFiles}}
		}, "triggers.meeting"},
		{"unknown source", func(c *ComposerConfig) {
			c.Triggers[ComposerContextChat]["@"] = []string{ComposerSourceFiles, "repos"}
		}, `triggers.chat."@"`},
		{"negative linger seconds", func(c *ComposerConfig) {
			c.StatusBar.SubagentLingerSeconds = -1
		}, "status_bar.subagent_linger_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultComposerConfig()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() err = nil, want it to contain %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() err = %q, want it to contain %q", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), ComposerFileName) {
				t.Errorf("Validate() err = %q, want it to name %s", err, ComposerFileName)
			}
		})
	}
}

func TestSaveComposerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", ComposerFileName)
	if err := SaveComposer(path, DefaultComposerConfig()); err != nil {
		t.Fatalf("SaveComposer() err = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	var loaded ComposerConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("saved file does not parse: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(loaded.Triggers, DefaultComposerConfig().Triggers) {
		t.Errorf("round-tripped triggers differ: %+v", loaded.Triggers)
	}
	for _, quoted := range []string{`'@':`, `'#':`} {
		if !strings.Contains(string(data), quoted) {
			t.Errorf("saved file does not quote the %s trigger so YAML stays parseable:\n%s", quoted, data)
		}
	}
}
