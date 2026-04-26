package cmd

import (
	"reflect"
	"strings"

	viper "github.com/spf13/viper"
)

// registerConfigDefaults walks cfg via mapstructure tags and calls
// v.SetDefault for every leaf field with a non-zero value. cfg must be a
// struct or pointer to struct annotated with `mapstructure` tags.
//
// This replaces the hand-maintained list of v.SetDefault calls and keeps
// viper's default registry automatically in sync with config.DefaultConfig().
func registerConfigDefaults(v *viper.Viper, cfg any) {
	walkConfigLeaves(reflect.ValueOf(cfg), "", func(path string, val reflect.Value) {
		if isLeafZeroValue(val) {
			return
		}
		v.SetDefault(path, val.Interface())
	})
}

// walkConfigLeaves traverses a struct (or pointer to struct) using
// mapstructure tags and invokes visit for every leaf field. Structs and
// non-nil pointers to structs are recursed into; everything else (scalars,
// slices, maps, pointers to scalars, nil pointers to structs) is treated as
// a leaf.
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
	for i := 0; i < t.NumField(); i++ {
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
		default:
			visit(full, fv)
		}
	}
}

// isLeafZeroValue reports whether v carries no useful default. Zero-valued
// leaves don't need to be registered with viper since GetXxx already returns
// the zero value of the requested type.
func isLeafZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	default:
		return v.IsZero()
	}
}
