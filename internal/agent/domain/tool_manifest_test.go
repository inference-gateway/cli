package domain

import (
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
read_only: true
plan_mode: allowed
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
		{name: "unknown plan mode", yaml: strings.Replace(validManifest, "plan_mode: allowed", "plan_mode: sometimes", 1), wantErr: "plan_mode"},
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
	if !manifest.ReadOnly || !manifest.AllowedInPlanMode() || manifest.OnlyInPlanMode() {
		t.Errorf("policy = %+v, want read-only and allowed (not only) in plan mode", manifest)
	}

	var zero ToolManifest
	if zero.RequireApproval != nil || zero.ReadOnly || zero.AllowedInPlanMode() {
		t.Errorf("zero manifest must inherit approval, not be read-only and be hidden in plan mode")
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

func TestParseCustomToolManifest_RequiresCommand(t *testing.T) {
	if _, err := ParseCustomToolManifest([]byte(validManifest)); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("error = %v, want a missing command to be rejected", err)
	}
	manifest, err := ParseCustomToolManifest([]byte(validManifest + "command: [echo-tool, run]\ntimeout: 10\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifest.Command) != 2 || manifest.Timeout != 10 {
		t.Errorf("manifest = %+v, want the command and timeout parsed", manifest)
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
