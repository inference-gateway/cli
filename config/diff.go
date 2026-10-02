package config

import (
	"reflect"
	"strings"
	"unicode"
)

// ChangedKeys lists the dotted keys whose values differ between two configs.
// Keys follow the yaml tags. Sections kept in their own file (tag "-") are
// named after their field in snake_case, e.g. tools.safety.approval_behaviour.
func ChangedKeys(before, after *Config) []string {
	var keys []string
	collectChangedKeys(reflect.ValueOf(before).Elem(), reflect.ValueOf(after).Elem(), "", &keys)
	return keys
}

func collectChangedKeys(before, after reflect.Value, prefix string, keys *[]string) {
	if before.Kind() != reflect.Struct {
		if !reflect.DeepEqual(before.Interface(), after.Interface()) {
			*keys = append(*keys, prefix)
		}
		return
	}
	for i := range before.NumField() {
		field := before.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		key := configKeyName(field)
		if prefix != "" {
			key = prefix + "." + key
		}
		collectChangedKeys(before.Field(i), after.Field(i), key, keys)
	}
}

func configKeyName(field reflect.StructField) string {
	if name, _, _ := strings.Cut(field.Tag.Get("yaml"), ","); name != "" && name != "-" {
		return name
	}
	var b strings.Builder
	prevLower := false
	for _, r := range field.Name {
		if unicode.IsUpper(r) && prevLower {
			b.WriteByte('_')
		}
		prevLower = unicode.IsLower(r)
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
