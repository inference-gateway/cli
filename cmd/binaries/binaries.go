package binaries

import (
	"fmt"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	audio "github.com/inference-gateway/cli/internal/audio"
)

func NewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "binaries",
		Short: "Manage the prebuilt tools (whisper-cli, ffmpeg, llama-tts) in ~/.infer/bin/tools",
		Long: `Install, upgrade and inspect the prebuilt tools published by
github.com/inference-gateway/binaries. The CLI is the single owner of these
binaries on a local machine: install.sh picks the host's asset, verifies it
against the release's checksums.txt and replaces any binary whose sha256
differs (a matching one is kept). Desktop and opentask check and install
through these commands instead of downloading on their own.`,
	}

	installCommand := &cobra.Command{
		Use:          "install [name...]",
		Short:        "Install or upgrade prebuilt binaries via install.sh (no names = all)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, _ := cmd.Flags().GetString("version")
			return audio.NewBinaryStore(config.SpeechToTextConfig{AutoDownload: true}).
				Install(cmd.Context(), version, args)
		},
	}
	installCommand.Flags().String("version", "", "pin a release tag (e.g. v0.5.0); default latest")

	statusCommand := &cobra.Command{
		Use:          "status [name...]",
		Short:        "Report each prebuilt binary as missing, stale or current (read-only)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			statuses, err := audio.NewBinaryStore(config.SpeechToTextConfig{}).Status(cmd.Context(), args)
			if err != nil {
				return err
			}
			notCurrent := 0
			for _, st := range statuses {
				line := fmt.Sprintf("%-12s %-8s %s", st.Name, st.State, st.Path)
				if st.Detail != "" {
					line += " (" + st.Detail + ")"
				}
				fmt.Println(line)
				if st.State != audio.BinaryCurrent {
					notCurrent++
				}
			}
			if notCurrent > 0 {
				return fmt.Errorf("%d of %d binaries are not current; run `infer binaries install`", notCurrent, len(statuses))
			}
			return nil
		},
	}

	command.AddCommand(installCommand, statusCommand)
	return command
}
