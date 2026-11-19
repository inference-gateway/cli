package utils

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	viper "github.com/spf13/viper"
	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
)

// WriteViperConfigWithIndent writes the current Viper config with specified indentation.
// It writes the full baseline, the same structure as config init, and keeps every key
// of the existing file that Config does not declare, so a setting another tool stores
// in config.yaml, such as the desktop app's projects.root, survives the write.
func WriteViperConfigWithIndent(v *viper.Viper, indent int) error {
	filename := v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("no config file is currently being used")
	}

	cfg := config.DefaultConfig()

	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	var doc yaml.Node
	if err := doc.Encode(cfg); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	if err := keepUndeclaredKeys(&doc, filename); err != nil {
		return err
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(&doc); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// keepUndeclaredKeys adds to doc each key of the YAML file at path that Config
// does not declare, at any depth. A key Config declares always comes from doc,
// even when doc omits it as empty. A missing or empty file has nothing to keep.
func keepUndeclaredKeys(doc *yaml.Node, path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	var existing yaml.Node
	if err := yaml.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if len(existing.Content) == 0 {
		return nil
	}
	addUndeclaredKeys(doc, existing.Content[0], reflect.TypeOf(config.Config{}))
	return nil
}

// addUndeclaredKeys appends to the mapping dst each key of the mapping src that
// the struct type t has no field for, and descends into the fields it has.
func addUndeclaredKeys(dst, src *yaml.Node, t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, value := src.Content[i], src.Content[i+1]
		field, declared := fieldForKey(t, key.Value)
		if !declared {
			dst.Content = append(dst.Content, key, value)
			continue
		}
		if child := mappingValue(dst, key.Value); child != nil {
			addUndeclaredKeys(child, value, field.Type)
		}
	}
}

// fieldForKey finds the field of the struct type t that a config key names: its
// yaml tag, or its mapstructure tag for a field kept out of config.yaml.
func fieldForKey(t reflect.Type, key string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "-" {
			name, _, _ = strings.Cut(field.Tag.Get("mapstructure"), ",")
		}
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		if name == key {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// mappingValue returns the value node of key in the mapping node, or nil.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// WriteViperConfigSparse writes only the keys present in v, without seeding from
// DefaultConfig. That keeps a project ./.infer/config.yaml a sparse override
// that inherits every unset key from ~/.infer/. WriteViperConfigWithIndent
// writes the full baseline instead.
func WriteViperConfigSparse(v *viper.Viper, indent int) error {
	filename := v.ConfigFileUsed()
	if filename == "" {
		return fmt.Errorf("no config file is currently being used")
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(v.AllSettings()); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filename, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
