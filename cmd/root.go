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
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Welcome to the Inference Gateway CLI!")
		fmt.Println("Use 'infer chat' to start interactive chat or --help to see available commands.")
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
	rootCmd.PersistentFlags().String("tools-bash-mode-all-allow-append", "",
		"comma/newline-separated commands appended to the bash allow-list baseline "+
			"(tools.bash.mode.all.allow); INFER_TOOLS_BASH_MODE_ALL_ALLOW_APPEND takes precedence")

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

// resolveBashAllowOverride returns the override value for a bash allow-list,
// preferring the env var over the matching persistent flag (per the documented
// flags < env layering). Empty means neither was provided.
func resolveBashAllowOverride(flagName, envName string) string {
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
// layers. The append never replaces the curated defaults — it only adds.
func applyBashAllowAppends(v *viper.Viper) {
	appends := []struct {
		key, appendFlag, appendEnv string
	}{
		{
			"tools.bash.mode.all.allow",
			"tools-bash-mode-all-allow-append", "INFER_TOOLS_BASH_MODE_ALL_ALLOW_APPEND",
		},
	}

	for _, a := range appends {
		if override := resolveBashAllowOverride(a.appendFlag, a.appendEnv); override != "" {
			v.Set(a.key, append(v.GetStringSlice(a.key), parseDelimitedList(override)...))
		}
	}
}

func initConfig() {
	V = viper.New()
	v := V

	registerConfigDefaults(v, config.DefaultConfig())

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./.infer")
	v.AddConfigPath("$HOME/.infer")
	v.SetEnvPrefix("INFER")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if a2aAgents := os.Getenv("INFER_A2A_AGENTS"); a2aAgents != "" {
		v.Set("a2a.agents", parseDelimitedList(a2aAgents))
	}

	if err := v.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		fmt.Fprintf(os.Stderr, "Error binding verbose flag: %v\n", err)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}
	}

	applyBashAllowAppends(v)

	cfg, err := loadConfigFromViper()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	Cfg = cfg

	verbose := v.GetBool("verbose")
	debug := v.GetBool("logging.debug")
	logDir := v.GetString("logging.dir")
	stdout := v.GetBool("logging.stdout")

	if logDir == "" {
		configFile := v.ConfigFileUsed()
		if configFile != "" {
			configDir := filepath.Dir(configFile)
			logDir = filepath.Join(configDir, "logs")
		}
	}

	logger.Init(logger.Config{
		Verbose: verbose,
		Debug:   debug,
		LogDir:  logDir,
		Stdout:  stdout,
	})
}
