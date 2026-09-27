package domain

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	sdk "github.com/inference-gateway/sdk"
)

// PlanMode says whether a tool may run in plan mode. It gates execution and
// approval, not advertisement: every mode advertises the same tool list so a
// mode switch never invalidates the provider's prompt cache.
type PlanMode string

const (
	PlanModeHidden  PlanMode = "hidden"
	PlanModeAllowed PlanMode = "allowed"
	PlanModeOnly    PlanMode = "only"
)

var toolNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// ToolManifest defines a tool: the JSON Schema the model sees and its default
// policy. A manifest without Command binds to the Go tool registered under the
// same name. A nil RequireApproval inherits tools.safety.require_approval, so
// the zero value (not read-only, hidden in plan mode) is the policy of any
// tool without a manifest, such as an MCP tool.
type ToolManifest struct {
	Name            string         `yaml:"name"`
	Description     string         `yaml:"description"`
	Command         []string       `yaml:"command,omitempty"`
	Parameters      map[string]any `yaml:"parameters"`
	RequireApproval *bool          `yaml:"require_approval,omitempty"`
	ReadOnly        bool           `yaml:"read_only,omitempty"`
	PlanMode        PlanMode       `yaml:"plan_mode,omitempty"`
	Timeout         int            `yaml:"timeout,omitempty"`
	Enabled         *bool          `yaml:"enabled,omitempty"`
}

// ParseToolManifest decodes and validates a manifest, rejecting unknown fields
// so a misspelled policy key fails loudly instead of silently defaulting.
func ParseToolManifest(data []byte) (ToolManifest, error) {
	var manifest ToolManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return ToolManifest{}, fmt.Errorf("decoding tool manifest: %w", err)
	}
	manifest.Parameters, _ = normalizeSchema(manifest.Parameters).(map[string]any)
	if err := manifest.Validate(); err != nil {
		return ToolManifest{}, err
	}
	return manifest, nil
}

// ParseCustomToolManifest parses a manifest loaded from disk, which must name
// the command that runs the tool.
func ParseCustomToolManifest(data []byte) (ToolManifest, error) {
	manifest, err := ParseToolManifest(data)
	if err != nil {
		return ToolManifest{}, err
	}
	if len(manifest.Command) == 0 {
		return ToolManifest{}, fmt.Errorf("tool %s: command is required for a custom tool", manifest.Name)
	}
	return manifest, nil
}

// Validate checks the name, the description, the plan mode and that the
// parameters are a JSON Schema object whose required fields are declared.
func (m ToolManifest) Validate() error {
	if !toolNamePattern.MatchString(m.Name) {
		return fmt.Errorf("tool name %q must match %s", m.Name, toolNamePattern)
	}
	if strings.TrimSpace(m.Description) == "" {
		return fmt.Errorf("tool %s: description is required", m.Name)
	}
	if !slices.Contains([]PlanMode{"", PlanModeHidden, PlanModeAllowed, PlanModeOnly}, m.PlanMode) {
		return fmt.Errorf("tool %s: plan_mode %q must be one of hidden, allowed, only", m.Name, m.PlanMode)
	}
	return m.validateParameters()
}

func (m ToolManifest) validateParameters() error {
	if m.Parameters["type"] != "object" {
		return fmt.Errorf("tool %s: parameters must be a JSON Schema of type object", m.Name)
	}
	properties, isMap := m.Parameters["properties"].(map[string]any)
	if !isMap && m.Parameters["properties"] != nil {
		return fmt.Errorf("tool %s: parameters.properties must be a map", m.Name)
	}
	rawRequired, hasRequired := m.Parameters["required"]
	if !hasRequired {
		return nil
	}
	required, isList := rawRequired.([]string)
	if !isList {
		return fmt.Errorf("tool %s: parameters.required must be a list of property names", m.Name)
	}
	for _, name := range required {
		if _, declared := properties[name]; !declared {
			return fmt.Errorf("tool %s: required parameter %q is not declared in properties", m.Name, name)
		}
	}
	return nil
}

// AllowedInPlanMode reports whether the tool may run in plan mode.
func (m ToolManifest) AllowedInPlanMode() bool {
	return m.PlanMode == PlanModeAllowed || m.PlanMode == PlanModeOnly
}

// OnlyInPlanMode reports whether the tool may run in plan mode alone.
func (m ToolManifest) OnlyInPlanMode() bool {
	return m.PlanMode == PlanModeOnly
}

// WithRequireApproval returns the manifest with the approval setting a user
// configured for the tool, when there is one.
func (m ToolManifest) WithRequireApproval(configured *bool) ToolManifest {
	if configured != nil {
		m.RequireApproval = configured
	}
	return m
}

// RequiresApproval reports whether a call needs approval: the tool's own
// setting when it has one, else the inherited global require_approval.
func (m ToolManifest) RequiresApproval(inherited bool) bool {
	if m.RequireApproval != nil {
		return *m.RequireApproval
	}
	return inherited
}

// OfferedInMode reports whether the tool is offered to the model in mode:
// plan mode offers the tools allowed there, read-only mode the read-only
// tools, and every other mode all tools except the plan-only ones.
func (m ToolManifest) OfferedInMode(mode AgentMode) bool {
	switch mode {
	case AgentModePlan:
		return m.AllowedInPlanMode()
	case AgentModeReadOnly:
		return m.ReadOnly
	default:
		return !m.OnlyInPlanMode()
	}
}

// Definition builds the definition sent to the model. A non-empty description,
// a user override from prompts.yaml, replaces the manifest's. The parameters
// are a deep copy, so the caller may adjust them.
func (m ToolManifest) Definition(description string) sdk.ChatCompletionTool {
	description = cmp.Or(description, m.Description)
	parameters, _ := cloneSchema(m.Parameters).(map[string]any)
	functionParameters := sdk.FunctionParameters(parameters)
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        m.Name,
			Description: &description,
			Parameters:  &functionParameters,
		},
	}
}

// ManifestTool is implemented by tools that bring their own manifest, such as
// the browser and computer tools, so the tool registry can answer their policy.
type ManifestTool interface {
	Manifest() ToolManifest
}

// ToolManifests are a bounded context's built-in tool manifests by tool name.
type ToolManifests map[string]ToolManifest

// Manifest returns the named tool's manifest. A tool without one gets a
// manifest carrying only its name, which is the default policy.
func (ms ToolManifests) Manifest(name string) ToolManifest {
	if manifest, ok := ms[name]; ok {
		return manifest
	}
	return ToolManifest{Name: name}
}

// LoadToolManifests parses every *.yaml file in dir of fsys. Each file is
// named after its tool, e.g. Read.yaml.
func LoadToolManifests(fsys fs.FS, dir string) (ToolManifests, error) {
	paths, err := fs.Glob(fsys, path.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	manifests := make(ToolManifests, len(paths))
	for _, manifestPath := range paths {
		data, err := fs.ReadFile(fsys, manifestPath)
		if err != nil {
			return nil, err
		}
		manifest, err := ParseToolManifest(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", manifestPath, err)
		}
		if fileName := strings.TrimSuffix(path.Base(manifestPath), ".yaml"); manifest.Name != fileName {
			return nil, fmt.Errorf("%s: tool name %q must match the file name", manifestPath, manifest.Name)
		}
		manifests[manifest.Name] = manifest
	}
	return manifests, nil
}

// MustLoadToolManifests is LoadToolManifests for embedded manifests, where a
// broken file is a programming error.
func MustLoadToolManifests(fsys fs.FS, dir string) ToolManifests {
	manifests, err := LoadToolManifests(fsys, dir)
	if err != nil {
		panic(err)
	}
	return manifests
}

// MustGet returns a built-in tool's manifest. A built-in tool without one is a
// programming error.
func (ms ToolManifests) MustGet(name string) ToolManifest {
	manifest, ok := ms[name]
	if !ok {
		panic(fmt.Sprintf("no manifest for built-in tool %q", name))
	}
	return manifest
}

// PropertySchema returns the schema of one parameter of def so a tool can fill
// in values only known at runtime, such as options from its configuration.
func PropertySchema(def sdk.ChatCompletionTool, property string) map[string]any {
	properties, _ := (*def.Function.Parameters)["properties"].(map[string]any)
	schema, ok := properties[property].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("tool %s declares no parameter %q", def.Function.Name, property))
	}
	return schema
}

func normalizeSchema(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			value[key] = normalizeSchema(item)
		}
		return value
	case []any:
		if strs, ok := stringsOf(value); ok {
			return strs
		}
		for i, item := range value {
			value[i] = normalizeSchema(item)
		}
		return value
	}
	return value
}

func stringsOf(items []any) ([]string, bool) {
	strs := make([]string, 0, len(items))
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			return nil, false
		}
		strs = append(strs, str)
	}
	return strs, true
}

func cloneSchema(value any) any {
	switch value := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(value))
		for key, item := range value {
			clone[key] = cloneSchema(item)
		}
		return clone
	case []any:
		clone := make([]any, len(value))
		for i, item := range value {
			clone[i] = cloneSchema(item)
		}
		return clone
	case []string:
		return slices.Clone(value)
	}
	return value
}
