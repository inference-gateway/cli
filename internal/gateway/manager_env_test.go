package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// TestManager_loadEnvironment verifies the key precedence when assembling the
// gateway binary environment: system environment, then project .env, then the
// ~/.infer/auth.yaml fallback (first hit per key wins).
func TestManager_loadEnvironment(t *testing.T) {
	const (
		sysKey  = "INFER_TEST_SYS_KEY"
		authKey = "INFER_TEST_AUTH_KEY"
		bothKey = "INFER_TEST_BOTH_KEY"
	)

	setup := func(t *testing.T, dotEnv, authYAML string) []string {
		t.Helper()

		home := t.TempDir()
		t.Setenv("HOME", home)
		if authYAML != "" {
			require.NoError(t, os.MkdirAll(filepath.Join(home, config.ConfigDirName), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(home, config.ConfigDirName, config.AuthFileName), []byte(authYAML), 0600))
		}

		t.Chdir(t.TempDir())
		if dotEnv != "" {
			require.NoError(t, os.WriteFile(".env", []byte(dotEnv), 0644))
		}

		return (&Supervisor{}).loadEnvironment()
	}

	lookup := func(t *testing.T, env []string, key string) (string, bool) {
		t.Helper()
		for _, entry := range env {
			if k, v, ok := strings.Cut(entry, "="); ok && k == key {
				return v, true
			}
		}
		return "", false
	}

	t.Run("auth.yaml fallback applies with no .env and no system key", func(t *testing.T) {
		env := setup(t, "", authKey+": from-auth\n")

		value, ok := lookup(t, env, authKey)
		require.True(t, ok)
		require.Equal(t, "from-auth", value)
	})

	t.Run("system environment wins over .env and auth.yaml", func(t *testing.T) {
		t.Setenv(sysKey, "from-system")
		env := setup(t, sysKey+"=from-dotenv\n", sysKey+": from-auth\n")

		value, ok := lookup(t, env, sysKey)
		require.True(t, ok)
		require.Equal(t, "from-system", value)
	})

	t.Run("project .env wins over auth.yaml", func(t *testing.T) {
		env := setup(t, bothKey+"=from-dotenv\n", bothKey+": from-auth\n")

		value, ok := lookup(t, env, bothKey)
		require.True(t, ok)
		require.Equal(t, "from-dotenv", value)
	})

	t.Run("empty auth.yaml values are not passed", func(t *testing.T) {
		env := setup(t, "", authKey+`: ""`+"\n")

		_, ok := lookup(t, env, authKey)
		require.False(t, ok)
	})

	t.Run("missing auth.yaml degrades gracefully", func(t *testing.T) {
		env := setup(t, bothKey+"=from-dotenv\n", "")

		value, ok := lookup(t, env, bothKey)
		require.True(t, ok)
		require.Equal(t, "from-dotenv", value)
	})

	t.Run("malformed auth.yaml degrades gracefully", func(t *testing.T) {
		env := setup(t, bothKey+"=from-dotenv\n", "{{{{ not yaml")

		value, ok := lookup(t, env, bothKey)
		require.True(t, ok)
		require.Equal(t, "from-dotenv", value)
	})
}
