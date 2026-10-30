package configcmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"reflect"
	"strconv"
	"strings"

	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func newGetCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "get [key]",
		Short: "Get a configuration value",
		Long: `Print the effective value of a configuration key, or the whole config when no key is given.

The value reflects what the CLI actually runs with: built-in defaults, the userspace
~/.infer/config.yaml baseline merged key-by-key with the project .infer/config.yaml,
and INFER_* environment overrides.

Keys are dotted paths into the effective config:
  infer config get agent.model
  infer config get tools.bash
  infer config get                      # dump the whole effective config

Most keys come from config.yaml; the tools policy comes from ~/.infer/tools.yaml
and is shown here too.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return getConfigValue(state, cmd, args) },
	}
	output.AddFormatFlag(command, "yaml", "json")
	return command
}

func newSetCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value in config.yaml.

Keys are dotted paths into config.yaml. The value is parsed to the field's type
(bool, integer, number or string); list keys take a comma-separated value:
  infer config set agent.model openai/gpt-4o
  infer config set agent.max_turns 50
  infer config set agent.max_tokens 8192

The tools policy is not in config.yaml: edit ~/.infer/tools.yaml directly.

By default the userspace ~/.infer/config.yaml baseline is updated; pass --project
to write a sparse override into the project .infer/config.yaml instead.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setConfigValue(cmd, args, state.LoadConfig)
		},
	}
	runtime.AddProjectFlag(command)
	return command
}

// getConfigValue prints the effective value of a config key. The effective
// config is the fully resolved runtime config (defaults + merged config files + env),
// serialized to a generic map so any dotted key can be looked up uniformly.
func getConfigValue(state *runtime.State, cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")

	cfg := state.Config()
	if cfg == nil {
		return fmt.Errorf("configuration is not loaded")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to build config map: %w", err)
	}
	if err := injectTools(root, cfg.Tools); err != nil {
		return err
	}

	var value any = root
	if len(args) == 1 {
		v, err := lookupConfigKey(root, args[0])
		if err != nil {
			return err
		}
		value = v
	}

	return printConfigValue(cmd.OutOrStdout(), value, format)
}

// injectTools merges the tools policy into the get dump under the names the
// INFER_TOOLS_* overrides use, tools.<key> for the policy keys and the tool
// sections alike. Config.Tools is tagged yaml:"-", so yaml.Marshal(cfg) drops it.
func injectTools(root map[string]any, tools config.ToolsConfig) error {
	data, err := yaml.Marshal(tools)
	if err != nil {
		return fmt.Errorf("failed to serialize tools config: %w", err)
	}
	section := map[string]any{}
	if err := yaml.Unmarshal(data, &section); err != nil {
		return fmt.Errorf("failed to build tools config map: %w", err)
	}
	if sections, ok := section["tools"].(map[string]any); ok {
		delete(section, "tools")
		maps.Copy(section, sections)
	}
	root["tools"] = section
	return nil
}

// lookupConfigKey walks a dotted key into the generic config map.
func lookupConfigKey(root map[string]any, key string) (any, error) {
	parts := strings.Split(key, ".")
	var current any = root
	for i, part := range parts {
		section, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config key %q not found: %q is not a section", key, strings.Join(parts[:i], "."))
		}
		next, ok := section[part]
		if !ok {
			return nil, fmt.Errorf("config key %q not found", key)
		}
		current = next
	}
	return current, nil
}

func printConfigValue(w io.Writer, value any, format string) error {
	if format == "json" {
		return output.PrintJSON(w, value)
	}

	out, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to format value as yaml: %w", err)
	}
	fmt.Fprint(w, string(out))
	return nil
}

// setConfigValue parses value to the type of the target field (discovered by
// reflecting over the Config struct) and persists it to config.yaml. The
// userspace baseline (~/.infer) is updated by default; --project targets the
// project .infer/config.yaml. A write that load rejects is rolled back, so a
// bad value can never leave infer unable to start.
func setConfigValue(cmd *cobra.Command, args []string, load func() (*config.Config, error)) error {
	key := args[0]
	rawValue := args[1]

	if file, ok := runtime.SidecarFile(key); ok {
		return fmt.Errorf("config key %q lives in %s, not config.yaml; edit that file to change it", key, file)
	}

	kind, ok := resolveConfigKeyKind(key)
	if !ok {
		return fmt.Errorf("unknown config key %q (use a dotted path into config.yaml, e.g. agent.model)", key)
	}

	parsed, err := parseConfigValue(rawValue, kind)
	if err != nil {
		return fmt.Errorf("invalid value for %q: %w", key, err)
	}

	toProject := runtime.ProjectFlag(cmd)
	target, path, err := runtime.ConfigWriteTarget(toProject)
	if err != nil {
		return err
	}

	previous, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	target.Set(key, parsed)
	save := utils.WriteViperConfigWithIndent
	if toProject {
		save = utils.WriteViperConfigSparse
	}
	if err := save(target, 2); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if _, err := load(); err != nil {
		if restoreErr := restoreConfigFile(path, previous, existed); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restoring %s: %w", path, restoreErr))
		}
		return fmt.Errorf("%s = %v was not saved: %w", key, parsed, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", formatting.FormatSuccess(fmt.Sprintf("Set %s = %v", key, parsed)))
	fmt.Fprintf(cmd.OutOrStdout(), "Configuration saved to: %s\n", path)
	return nil
}

// restoreConfigFile puts back the file a rejected write replaced, or removes
// it when the write created it.
func restoreConfigFile(path string, previous []byte, existed bool) error {
	if !existed {
		return os.Remove(path)
	}
	return os.WriteFile(path, previous, 0o600)
}

// resolveConfigKeyKind walks the Config struct by mapstructure tag to find the
// kind of the field a dotted key points at. Returns false for unknown keys and
// for keys whose section lives in a split file rather than config.yaml.
func resolveConfigKeyKind(key string) (reflect.Kind, bool) {
	if _, sidecar := runtime.SidecarFile(key); sidecar {
		return reflect.Invalid, false
	}
	parts := strings.Split(key, ".")
	t := reflect.TypeOf(config.Config{})

	for i, part := range parts {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return reflect.Invalid, false
		}

		field, ok := fieldByConfigTag(t, part)
		if !ok {
			return reflect.Invalid, false
		}

		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if i == len(parts)-1 {
			return ft.Kind(), true
		}
		t = ft
	}
	return reflect.Invalid, false
}

// fieldByConfigTag finds the struct field whose mapstructure tag matches name.
// Falls back to the lowercased field name when no tag is present.
func fieldByConfigTag(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("mapstructure")
		if tag == "" {
			tag = strings.ToLower(f.Name)
		}
		if comma := strings.Index(tag, ","); comma >= 0 {
			tag = tag[:comma]
		}
		if tag == "-" {
			continue
		}
		if tag == name {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

func parseConfigValue(raw string, kind reflect.Kind) (any, error) {
	switch kind {
	case reflect.String:
		return raw, nil
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("expected a boolean (true/false)")
		}
		return b, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected an integer")
		}
		return n, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected a non-negative integer")
		}
		return n, nil
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("expected a number")
		}
		return f, nil
	case reflect.Slice:
		return runtime.ParseDelimitedList(raw), nil
	default:
		return nil, fmt.Errorf("setting %s values is not supported via config set", kind)
	}
}
