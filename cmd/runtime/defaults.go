package runtime

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
)

// registerConfigDefaults walks cfg via mapstructure tags and calls v.SetDefault
// for every leaf, zero values included, so each key is known to Viper and an
// INFER_* override for it reaches Unmarshal.
func registerConfigDefaults(v *viper.Viper, cfg any) {
	walkConfigLeaves(reflect.ValueOf(cfg), "", func(path string, val reflect.Value) {
		v.SetDefault(path, val.Interface())
	})
	registerComposerTriggerDefaults(v)
}

// registerComposerTriggerDefaults flattens the default trigger table into
// per-leaf defaults. Viper treats a nested map of non-any type as an opaque
// leaf and replaces it wholesale, so a composer.yaml overriding one trigger
// would otherwise drop the rest of the default table.
func registerComposerTriggerDefaults(v *viper.Viper) {
	for _, context := range slices.Sorted(maps.Keys(config.DefaultComposerConfig().Triggers)) {
		table := config.DefaultComposerConfig().Triggers[context]
		for _, trigger := range slices.Sorted(maps.Keys(table)) {
			v.SetDefault("composer.triggers."+context+"."+trigger, slices.Clone(table[trigger]))
		}
	}
}

// walkConfigLeaves traverses a struct (or pointer to struct) using mapstructure
// tags and invokes visit for every leaf field. Structs, non-nil pointers to
// structs and string-keyed maps of structs are recursed into. Everything else
// is a leaf.
func walkConfigLeaves(v reflect.Value, prefix string, visit func(path string, val reflect.Value)) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			continue
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		fv := v.Field(i)
		switch {
		case fv.Kind() == reflect.Struct:
			walkConfigLeaves(fv, full, visit)
		case fv.Kind() == reflect.Pointer && fv.Type().Elem().Kind() == reflect.Struct:
			if fv.IsNil() {
				visit(full, fv)
			} else {
				walkConfigLeaves(fv.Elem(), full, visit)
			}
		case isStructMap(fv.Type()) && fv.Len() > 0:
			for _, key := range fv.MapKeys() {
				walkConfigLeaves(fv.MapIndex(key), full+"."+key.String(), visit)
			}
		default:
			visit(full, fv)
		}
	}
}

func isStructMap(t reflect.Type) bool {
	return t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Struct
}
