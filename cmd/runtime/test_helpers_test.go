package runtime

import (
	"fmt"

	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

var (
	testState = NewState()
	rootCmd   = newTestRootCommand()
	V         *viper.Viper
	Cfg       *config.Config
	loggerCfg logger.Config
)

func newTestRootCommand() *cobra.Command {
	command := &cobra.Command{Use: "infer"}
	command.PersistentFlags().BoolP("verbose", "v", false, "")
	command.PersistentFlags().String("tools-bash-allow-append", "", "")
	command.PersistentFlags().String("reminders-file", "", "")
	return command
}

func initConfig() {
	testState = NewState()
	if err := testState.Initialize(rootCmd); err != nil {
		panic(fmt.Sprintf("initializing test config: %v", err))
	}
	V = testState.Viper()
	Cfg = testState.Config()
	loggerCfg = testState.loggerCfg
}

func disableStdoutLogging() {
	testState.loggerCfg = loggerCfg
	testState.DisableStdoutLogging()
	loggerCfg = testState.loggerCfg
}
