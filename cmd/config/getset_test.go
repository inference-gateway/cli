package configcmd

import (
	"reflect"
	"strings"
	"testing"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
)

func TestResolveConfigKeyKind(t *testing.T) {
	cases := []struct {
		key  string
		kind reflect.Kind
		ok   bool
	}{
		{"agent.model", reflect.String, true},
		{"tools.bash.enabled", reflect.Invalid, false},
		{"agent.max_turns", reflect.Int, true},
		{"gateway.timeout", reflect.Int, true},
		{"tools.sandbox.directories", reflect.Invalid, false},
		{"nonexistent", reflect.Invalid, false},
		{"tools.nope.enabled", reflect.Invalid, false},
		{"agent.model.deeper", reflect.Invalid, false},
	}

	for _, c := range cases {
		kind, ok := resolveConfigKeyKind(c.key)
		if ok != c.ok {
			t.Errorf("%s: ok=%v, want %v", c.key, ok, c.ok)
			continue
		}
		if ok && kind != c.kind {
			t.Errorf("%s: kind=%v, want %v", c.key, kind, c.kind)
		}
	}
}

func TestSetConfigValueRejectsToolsKeys(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("project", false, "")

	for _, key := range []string{"tools", "tools.bash.enabled", "tools.safety.require_approval"} {
		err := setConfigValue(cmd, []string{key, "true"}, nil)
		if err == nil {
			t.Errorf("expected error for %q, got nil", key)
			continue
		}
		if !strings.Contains(err.Error(), "tools.yaml") {
			t.Errorf("error for %q should name tools.yaml, got %q", key, err)
		}
	}
}

func TestInjectTools(t *testing.T) {
	tools := config.DefaultToolsConfig()
	tools.WebSearch.MaxResults = 42

	root := map[string]any{"agent": map[string]any{"model": "gpt"}}
	if err := injectTools(root, *tools); err != nil {
		t.Fatalf("injectTools: %v", err)
	}

	section, ok := root["tools"].(map[string]any)
	if !ok {
		t.Fatalf("tools section missing or not a map: %#v", root["tools"])
	}
	webSearch, ok := section["web_search"].(map[string]any)
	if !ok {
		t.Fatalf("web_search missing: %#v", section)
	}
	if got := webSearch["max_results"]; got != 42 {
		t.Fatalf("max_results = %v, want 42", got)
	}
}

func TestParseConfigValue(t *testing.T) {
	if v, err := parseConfigValue("true", reflect.Bool); err != nil || v != true {
		t.Fatalf("bool: got %v, err %v", v, err)
	}
	if v, err := parseConfigValue("50", reflect.Int); err != nil || v.(int64) != 50 {
		t.Fatalf("int: got %v, err %v", v, err)
	}
	if v, err := parseConfigValue("openai/gpt-4o", reflect.String); err != nil || v.(string) != "openai/gpt-4o" {
		t.Fatalf("string: got %v, err %v", v, err)
	}

	v, err := parseConfigValue(".,/tmp, /data ", reflect.Slice)
	if err != nil {
		t.Fatalf("slice: unexpected err %v", err)
	}
	if got, want := v.([]string), []string{".", "/tmp", "/data"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("slice: got %v, want %v", got, want)
	}

	if _, err := parseConfigValue("notabool", reflect.Bool); err == nil {
		t.Error("expected error for invalid bool")
	}
	if _, err := parseConfigValue("abc", reflect.Int); err == nil {
		t.Error("expected error for invalid int")
	}
}

func TestLookupConfigKey(t *testing.T) {
	root := map[string]any{
		"agent": map[string]any{"model": "gpt", "max_turns": 50},
		"tools": map[string]any{"sandbox": map[string]any{"directories": []any{".", "/tmp"}}},
	}

	if v, err := lookupConfigKey(root, "agent.model"); err != nil || v != "gpt" {
		t.Fatalf("agent.model: got %v, err %v", v, err)
	}
	if _, err := lookupConfigKey(root, "tools.sandbox.directories"); err != nil {
		t.Fatalf("directories: unexpected err %v", err)
	}
	if _, err := lookupConfigKey(root, "agent.missing"); err == nil {
		t.Error("expected not-found error")
	}
	if _, err := lookupConfigKey(root, "agent.model.x"); err == nil {
		t.Error("expected error descending into a scalar")
	}
}

func TestSplitListValue(t *testing.T) {
	got := splitListValue("a, b ,,c\nd")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := splitListValue("  "); len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}
