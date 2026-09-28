package domain

import (
	"maps"
	"strings"
	"testing"
	"testing/fstest"
)

const validManifest = `name: Echo
description: Echoes its input.
parameters:
  type: object
  properties:
    text:
      type: string
      enum: [a, b]
  required:
    - text
require_approval: false
modes: [plan, readonly]
`

func TestParseToolManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{name: "valid", yaml: validManifest},
		{name: "invalid name", yaml: strings.Replace(validManifest, "name: Echo", "name: 1Echo", 1), wantErr: "must match"},
		{name: "missing description", yaml: strings.Replace(validManifest, "description: Echoes its input.", "description: ''", 1), wantErr: "description is required"},
		{name: "unknown field", yaml: validManifest + "readonly: true\n", wantErr: "field readonly not found"},
		{name: "parameters not an object", yaml: strings.Replace(validManifest, "type: object", "type: array", 1), wantErr: "type object"},
		{name: "undeclared required parameter", yaml: strings.Replace(validManifest, "- text", "- other", 1), wantErr: `"other" is not declared`},
		{name: "unknown agent mode", yaml: strings.Replace(validManifest, "readonly]", "sometimes]", 1), wantErr: `unknown agent mode "sometimes"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToolManifest([]byte(tt.yaml))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseToolManifest_Policy(t *testing.T) {
	manifest, err := ParseToolManifest([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.RequireApproval == nil || *manifest.RequireApproval {
		t.Errorf("RequireApproval = %v, want explicit false", manifest.RequireApproval)
	}
	if zero := (ToolManifest{}); zero.RequireApproval != nil {
		t.Errorf("zero manifest must inherit approval")
	}
}

func TestToolManifest_AvailableIn(t *testing.T) {
	manifest, err := ParseToolManifest([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		manifest ToolManifest
		mode     AgentMode
		want     bool
	}{
		{name: "listed mode", manifest: manifest, mode: AgentModePlan, want: true},
		{name: "unlisted mode", manifest: manifest, mode: AgentModeStandard, want: false},
		{name: "no modes means standard", mode: AgentModeStandard, want: true},
		{name: "no modes means auto", mode: AgentModeAutoAccept, want: true},
		{name: "no modes means auto with judge", mode: AgentModeAutoWithJudge, want: true},
		{name: "no modes excludes plan", mode: AgentModePlan, want: false},
		{name: "no modes excludes readonly", mode: AgentModeReadOnly, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.manifest.AvailableIn(tt.mode); got != tt.want {
				t.Errorf("AvailableIn(%s) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestToolManifest_RequiresApproval(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name       string
		manifest   ToolManifest
		configured *bool
		inherited  bool
		want       bool
	}{
		{name: "inherits the global setting", inherited: true, want: true},
		{name: "manifest default beats the global setting", manifest: ToolManifest{RequireApproval: &no}, inherited: true, want: false},
		{name: "configured setting beats the manifest default", manifest: ToolManifest{RequireApproval: &no}, configured: &yes, want: true},
		{name: "configured setting beats the global setting", configured: &no, inherited: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.manifest.WithRequireApproval(tt.configured).RequiresApproval(tt.inherited); got != tt.want {
				t.Errorf("RequiresApproval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolManifest_Definition(t *testing.T) {
	manifest, err := ParseToolManifest([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}

	def := manifest.Definition()
	if *def.Function.Description != "Echoes its input." {
		t.Errorf("description = %q, want the manifest's", *def.Function.Description)
	}

	required, ok := (*def.Function.Parameters)["required"].([]string)
	if !ok || required[0] != "text" {
		t.Fatalf("required = %#v, want []string{\"text\"}", (*def.Function.Parameters)["required"])
	}
	PropertySchema(def, "text")["enum"].([]string)[0] = "changed"
	required[0] = "changed"
	fresh := manifest.Definition()
	if PropertySchema(fresh, "text")["enum"].([]string)[0] != "a" || (*fresh.Function.Parameters)["required"].([]string)[0] != "text" {
		t.Error("modifying a definition must not leak into the manifest")
	}
}

func TestLoadToolManifests(t *testing.T) {
	manifests, err := LoadToolManifests(fstest.MapFS{"tools/Echo.yaml": {Data: []byte(validManifest)}}, "tools")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := manifests["Echo"]; !ok {
		t.Fatalf("manifests = %v, want Echo", manifests)
	}

	_, err = LoadToolManifests(fstest.MapFS{"tools/Other.yaml": {Data: []byte(validManifest)}}, "tools")
	if err == nil || !strings.Contains(err.Error(), "must match the file name") {
		t.Fatalf("error = %v, want a file named after another tool to be rejected", err)
	}
}

func TestDecodeToolManifest_InlineExtension(t *testing.T) {
	type extended struct {
		ToolManifest `yaml:",inline"`
		Command      []string `yaml:"command"`
	}
	var manifest extended
	if err := DecodeToolManifest([]byte(validManifest+"command:\n  - echo\n"), &manifest, &manifest.ToolManifest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.Name != "Echo" || manifest.Command[0] != "echo" {
		t.Errorf("decoded %+v, want the embedded manifest and the extra field", manifest)
	}
	if _, ok := manifest.Parameters["required"].([]string); !ok {
		t.Errorf("required = %#v, want the normalized []string", manifest.Parameters["required"])
	}

	err := DecodeToolManifest([]byte(validManifest+"read_only: true\n"), &manifest, &manifest.ToolManifest)
	if err == nil || !strings.Contains(err.Error(), "field read_only not found") {
		t.Errorf("error = %v, want an unknown field rejected", err)
	}

	if _, err := ParseToolManifest([]byte(validManifest + "command:\n  - echo\n")); err == nil {
		t.Error("a built-in manifest must reject the custom-tool command field")
	}
}

func TestValidateArguments(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
	}
	withRequired := func(required any) map[string]any {
		s := maps.Clone(schema)
		s["required"] = required
		return s
	}
	tests := []struct {
		name    string
		schema  map[string]any
		args    map[string]any
		wantErr string
	}{
		{name: "valid", schema: schema, args: map[string]any{"text": "hi", "count": float64(2)}},
		{name: "nil arguments", schema: schema, args: nil, wantErr: "cannot be nil"},
		{name: "no schema", schema: nil, args: map[string]any{"anything": true}},
		{name: "missing required from a manifest", schema: withRequired([]string{"text"}), args: map[string]any{}, wantErr: `required field "text" is missing`},
		{name: "missing required from JSON", schema: withRequired([]any{"text"}), args: map[string]any{}, wantErr: `required field "text" is missing`},
		{name: "wrong type", schema: schema, args: map[string]any{"text": float64(1)}, wantErr: `field "text" has invalid type: expected string, got number`},
		{name: "undeclared property", schema: schema, args: map[string]any{"other": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateArguments(tt.schema, tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
