package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"
	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	utils "github.com/inference-gateway/cli/internal/utils"
)

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a configuration value",
	Long: `Print the effective value of a configuration key, or the whole config when no key is given.

The value reflects what the CLI actually runs with: built-in defaults, the userspace
~/.infer/config.yaml baseline merged key-by-key with the project .infer/config.yaml,
and INFER_* environment overrides.

Keys are dotted paths into config.yaml:
  infer config get agent.model
  infer config get tools.sandbox.directories
  infer config get tools.bash
  infer config get                      # dump the whole effective config`,
	Args: cobra.MaximumNArgs(1),
	RunE: getConfigValue,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value in config.yaml.

Keys are dotted paths into config.yaml. The value is parsed to the field's type
(bool, integer, number or string); list keys take a comma-separated value:
  infer config set agent.model openai/gpt-4o
  infer config set tools.bash.enabled true
  infer config set agent.max_turns 50
  infer config set tools.sandbox.directories ".,/tmp,/data"

By default the userspace ~/.infer/config.yaml baseline is updated; pass --project
to write a sparse override into the project .infer/config.yaml instead.`,
	Args: cobra.ExactArgs(2),
	RunE: setConfigValue,
}

func init() {
	configGetCmd.Flags().StringP("format", "f", "yaml", "Output format (yaml, json)")

	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
}

// getConfigValue prints the effective value of a config key. The effective
// config is the fully resolved cmd.Cfg (defaults + merged config files + env),
// serialized to a generic map so any dotted key can be looked up uniformly.
func getConfigValue(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")

	if Cfg == nil {
		return fmt.Errorf("configuration is not loaded")
	}

	data, err := yaml.Marshal(Cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	root := map[string]any{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to build config map: %w", err)
	}

	var value any = root
	if len(args) == 1 {
		v, err := lookupConfigKey(root, args[0])
		if err != nil {
			return err
		}
		value = v
	}

	return printConfigValue(value, format)
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

func printConfigValue(value any, format string) error {
	if format == "json" {
		out, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format value as json: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	out, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to format value as yaml: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

// setConfigValue parses value to the type of the target field (discovered by
// reflecting over the Config struct) and persists it to config.yaml. The
// userspace baseline (~/.infer) is updated by default; --project targets the
// project .infer/config.yaml.
func setConfigValue(cmd *cobra.Command, args []string) error {
	key := args[0]
	rawValue := args[1]

	kind, ok := resolveConfigKeyKind(key)
	if !ok {
		return fmt.Errorf("unknown config key %q (use a dotted path into config.yaml, e.g. agent.model)", key)
	}

	parsed, err := parseConfigValue(rawValue, kind)
	if err != nil {
		return fmt.Errorf("invalid value for %q: %w", key, err)
	}

	toProject := GetProjectFlag(cmd)
	target, path, err := configWriteTarget(toProject)
	if err != nil {
		return err
	}

	target.Set(key, parsed)
	// The project layer is a sparse override (only the keys it sets); the home
	// baseline is written full so it remains a complete, ordered config.yaml.
	writeErr := utils.WriteViperConfigWithIndent(target, 2)
	if toProject {
		writeErr = utils.WriteViperConfigSparse(target, 2)
	}
	if writeErr != nil {
		return fmt.Errorf("failed to save config: %w", writeErr)
	}

	fmt.Printf("%s\n", formatting.FormatSuccess(fmt.Sprintf("Set %s = %v", key, parsed)))
	fmt.Printf("Configuration saved to: %s\n", path)
	return nil
}

// configWriteTarget returns a fresh viper bound to the file `config set` should
// write, plus that path. Writes target the userspace baseline
// (~/.infer/config.yaml) by default; --project (toProject) writes a sparse
// override into the project .infer/config.yaml. Either way the existing file is
// pre-loaded into a fresh viper so only the single key being set is added - the
// merged/effective config is never written back, keeping both files sparse.
func configWriteTarget(toProject bool) (*viper.Viper, string, error) {
	var path string
	if toProject {
		path = config.DefaultConfigPath
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		path = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	}

	wv := viper.New()
	wv.SetConfigFile(path)
	if _, statErr := os.Stat(path); statErr == nil {
		if err := wv.ReadInConfig(); err != nil {
			return nil, "", fmt.Errorf("failed to read %s: %w", path, err)
		}
	}
	return wv, path, nil
}

// resolveConfigKeyKind walks the Config struct by mapstructure tag to find the
// kind of the field a dotted key points at. Returns false for unknown keys and
// for keys whose section is excluded from config.yaml (mapstructure:"-").
func resolveConfigKeyKind(key string) (reflect.Kind, bool) {
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
// Falls back to the lowercased field name when no tag is present, and skips
// fields tagged "-" (split-file configs that do not live in config.yaml).
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
		return splitListValue(raw), nil
	default:
		return nil, fmt.Errorf("setting %s values is not supported via config set", kind)
	}
}

// splitListValue parses a comma/newline-separated value into a string slice,
// matching the split rule used for INFER_* list environment variables.
func splitListValue(raw string) []string {
	out := []string{}
	for _, item := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == '\n' }) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
