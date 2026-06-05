package cmd

import (
	"reflect"
	"testing"
)

func TestResolveConfigKeyKind(t *testing.T) {
	cases := []struct {
		key  string
		kind reflect.Kind
		ok   bool
	}{
		{"agent.model", reflect.String, true},
		{"tools.bash.enabled", reflect.Bool, true},
		{"agent.max_turns", reflect.Int, true},
		{"gateway.timeout", reflect.Int, true},
		{"tools.sandbox.directories", reflect.Slice, true},
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
