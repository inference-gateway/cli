package cmd

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestViperDefaultsCoverNonZeroConfigDefaults walks config.DefaultConfig() and
// asserts that every leaf with a non-zero default value has a corresponding
// entry in viper's AllKeys() after initConfig(). Catches drift between the
// canonical defaults in config/config.go and the SetDefault block in
// initConfig() in cmd/root.go.
func TestViperDefaultsCoverNonZeroConfigDefaults(t *testing.T) {
	withHermeticEnv(t)

	initConfig()

	allKeys := make(map[string]struct{})
	for _, k := range V.AllKeys() {
		allKeys[strings.ToLower(k)] = struct{}{}
	}

	leaves := collectLeaves(reflect.ValueOf(config.DefaultConfig()).Elem())

	var missing []string
	for _, l := range leaves {
		if isLeafZeroValue(l.value) {
			continue
		}
		if _, ok := allKeys[strings.ToLower(l.path)]; !ok {
			missing = append(missing, l.path)
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing,
		"%d leaf paths from config.DefaultConfig() have no viper default registered after initConfig():\n  %s",
		len(missing), strings.Join(missing, "\n  "))
}

// TestViperUnmarshalReproducesDefaultConfig verifies the stronger contract:
// after initConfig() with no config file and no env overrides, unmarshaling
// viper into an empty Config produces a value semantically equal to
// config.DefaultConfig() at every non-zero leaf. Catches both missing defaults
// AND mismatched values (e.g. a SetDefault that drifted out of sync with
// DefaultConfig()).
func TestViperUnmarshalReproducesDefaultConfig(t *testing.T) {
	withHermeticEnv(t)

	initConfig()

	actual := &config.Config{}
	require.NoError(t, V.Unmarshal(actual))

	expected := config.DefaultConfig()

	expectedLeaves := flattenLeaves(reflect.ValueOf(expected).Elem())
	actualLeaves := flattenLeaves(reflect.ValueOf(actual).Elem())

	var diffs []string
	for path, expVal := range expectedLeaves {
		if isZeroishAny(expVal) {
			continue
		}
		actVal, ok := actualLeaves[path]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: missing in unmarshaled config", path))
			continue
		}
		if !reflect.DeepEqual(expVal, actVal) {
			diffs = append(diffs, fmt.Sprintf("%s: expected %v, got %v", path, expVal, actVal))
		}
	}
	sort.Strings(diffs)
	assert.Empty(t, diffs,
		"%d leaf paths differ between DefaultConfig() and viper Unmarshal() output:\n  %s",
		len(diffs), strings.Join(diffs, "\n  "))
}

type leafEntry struct {
	path  string
	value reflect.Value
}

func collectLeaves(v reflect.Value) []leafEntry {
	var out []leafEntry
	walkConfigLeaves(v, "", func(path string, val reflect.Value) {
		out = append(out, leafEntry{path: path, value: val})
	})
	return out
}

// flattenLeaves walks v and returns a map of dotted leaf path to typed value
// for use in DeepEqual comparisons.
func flattenLeaves(v reflect.Value) map[string]any {
	out := make(map[string]any)
	for _, l := range collectLeaves(v) {
		out[l.path] = l.value.Interface()
	}
	return out
}

func isZeroishAny(v any) bool {
	if v == nil {
		return true
	}
	return isLeafZeroValue(reflect.ValueOf(v))
}

// withHermeticEnv unsets all INFER_* environment variables, points HOME at a
// temporary directory, and chdirs there so that initConfig() doesn't pick up
// the developer's actual config file or environment overrides.
func withHermeticEnv(t *testing.T) {
	t.Helper()

	saved := make(map[string]string)
	for _, env := range os.Environ() {
		key, val, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "INFER_") {
			saved[key] = val
			require.NoError(t, os.Unsetenv(key))
		}
	}
	t.Cleanup(func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
	})

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}
