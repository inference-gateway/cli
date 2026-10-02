package sandboxinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// PersistDirectory appends dir to tools.sandbox.directories in the userspace
// ~/.infer/config.yaml. current seeds the key when the file does not declare
// it, so persisting never shrinks the effective allow-list.
func PersistDirectory(dir string, current []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	path := filepath.Join(home, config.ConfigDirName, config.ConfigFileName)

	v := viper.New()
	v.SetConfigFile(path)
	if _, err := os.Stat(path); err == nil {
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
	}

	dirs := v.GetStringSlice("tools.sandbox.directories")
	if len(dirs) == 0 {
		dirs = slices.Clone(current)
	}
	if slices.Contains(dirs, dir) {
		return nil
	}
	v.Set("tools.sandbox.directories", append(dirs, dir))
	return utils.WriteViperConfigWithIndent(v, 2)
}
