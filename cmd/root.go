package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	fang "charm.land/fang/v2"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Global Viper instance and resolved Config used by every command. Both
// are populated exactly once by initConfig() at startup. Commands read
// Cfg directly instead of re-unmarshalling viper.
var (
	V   *viper.Viper
	Cfg *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "infer",
	Short: "The CLI for the Inference Gateway",
	Long: `A powerful command-line interface for managing and interacting with
the Inference Gateway. This CLI provides tools for configuration,
deployment, monitoring, and management of inference services.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Welcome to the Inference Gateway CLI!")
		fmt.Println("Use 'infer chat' to start interactive chat or --help to see available commands.")
		return nil
	},
}

func Execute() {
	defer logger.Close()

	if err := fang.Execute(context.Background(), rootCmd, fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().String("tools-bash-allow-append", "",
		"comma/newline-separated commands added to the bash allow-list in every mode "+
			"(standard, plan, auto); INFER_TOOLS_BASH_ALLOW_APPEND takes precedence")
	rootCmd.PersistentFlags().String("reminders-file", "",
		"path to a reminders YAML file, overriding project .infer/ and ~/.infer reminders.yaml "+
			"(INFER_REMINDERS_CONFIG inline YAML takes precedence)")

	cobra.OnInitialize(initConfig)
}

// parseDelimitedList splits a comma/newline-separated env value into trimmed,
// non-empty entries. Used for INFER_A2A_AGENTS and the bash allow-list append
// override (tools.bash.mode.all.allow), neither of which viper can parse
// generically into a slice from a single env var.
func parseDelimitedList(value string) []string {
	var out []string
	for _, item := range strings.FieldsFunc(value, func(c rune) bool {
		return c == ',' || c == '\n'
	}) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// resolveFlagEnvOverride returns the override value for a flag/env pair,
// preferring the env var over the matching persistent flag (per the documented
// flags < env layering). Empty means neither was provided.
func resolveFlagEnvOverride(flagName, envName string) string {
	if env := os.Getenv(envName); env != "" {
		return env
	}
	if val, err := rootCmd.PersistentFlags().GetString(flagName); err == nil {
		return val
	}
	return ""
}

// applyBashAllowAppends merges flag/env-supplied commands onto the bash
// allow-list already resolved from defaults and config files. The config-file
// list (tools.bash.mode.all.allow) is the every-mode baseline that bashAllowFor
// unions into every mode, so appending here makes the extra commands auto-run in
// standard, auto, and plan alike without touching the matcher. Must run after
// ReadInConfig so the append sees config-file values; v.Set then wins over later
// layers. The append never replaces the curated defaults - it only adds.
func applyBashAllowAppends(v *viper.Viper) {
	appends := []struct {
		key, appendFlag, appendEnv string
	}{
		{
			"tools.bash.mode.all.allow",
			"tools-bash-allow-append", "INFER_TOOLS_BASH_ALLOW_APPEND",
		},
	}

	for _, a := range appends {
		if override := resolveFlagEnvOverride(a.appendFlag, a.appendEnv); override != "" {
			v.Set(a.key, append(v.GetStringSlice(a.key), parseDelimitedList(override)...))
		}
	}
}

// loadLayeredConfig reads config.yaml userspace-first: the home
// ~/.infer/config.yaml is the base layer and a project ./.infer/config.yaml
// (or ./config.yaml) is merged on top key-by-key. This is the userspace-first
// model (issue #680) - a project commits only the keys it overrides and inherits
// everything else from the home baseline. Net precedence: defaults < home <
// project < flags < env. A project that omits config.yaml inherits home wholesale.
func loadLayeredConfig(v *viper.Viper) {
	homeConfigPath := ""
	if homeDir, err := os.UserHomeDir(); err == nil {
		homeConfigPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	}

	readLayer := func(path string, merge bool) {
		v.SetConfigFile(path)
		var err error
		if merge {
			err = v.MergeInConfig()
		} else {
			err = v.ReadInConfig()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config %s: %v\n", path, err)
			os.Exit(1)
		}
	}

	loaded := false
	if homeConfigPath != "" && fileExists(homeConfigPath) {
		readLayer(homeConfigPath, false)
		loaded = true
	}

	// The project layer is merged on top of home. When home and project resolve
	// to the same file (e.g. running from within ~/.infer), skip the second read
	// so a single file isn't merged onto itself.
	if projectPath := resolveProjectConfigPath(); projectPath != "" && !sameConfigFile(projectPath, homeConfigPath) {
		readLayer(projectPath, loaded)
	}
}

// resolveProjectConfigPath returns the first existing project-level config.yaml,
// matching the legacy search order (cwd ./config.yaml, then ./.infer/config.yaml).
// Returns "" when neither exists.
func resolveProjectConfigPath() string {
	for _, p := range []string{config.ConfigFileName, config.DefaultConfigPath} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func initConfig() {
	V = viper.New()
	v := V

	registerConfigDefaults(v, config.DefaultConfig())

	v.SetConfigType("yaml")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if a2aAgents := os.Getenv("INFER_A2A_AGENTS"); a2aAgents != "" {
		v.Set("a2a.agents", parseDelimitedList(a2aAgents))
	}

	if err := v.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding verbose flag: %v\n", err)
	}

	loadLayeredConfig(v)

	applyBashAllowAppends(v)

	cfg, err := loadConfigFromViper()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	Cfg = cfg

	if sp := os.Getenv("INFER_SUBAGENT_SYSTEM_PROMPT"); sp != "" {
		cfg.Prompts.Agent.SystemPrompt = sp
	}

	verbose := v.GetBool("verbose")
	debug := v.GetBool("logging.debug")
	logDir := v.GetString("logging.dir")
	stdout := v.GetBool("logging.stdout")

	if logDir == "" {
		logDir = config.DefaultLogsPath
	}

	logger.Init(logger.Config{
		Verbose: verbose,
		Debug:   debug,
		LogDir:  logDir,
		Stdout:  stdout,
	})
}
.Init(logger.Config{
		Verbose: verbose,
		Debug:   debug,
		LogDir:  logDir,
		Stdout:  stdout,
	})
}
