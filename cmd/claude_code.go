package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var claudeCodeCmd = &cobra.Command{
	Use:   "claude-code",
	Short: "Manage Claude Code CLI integration",
	Long: `Commands for managing Claude Code CLI authentication and configuration.

Claude Code mode uses your Claude Max/Pro subscription instead of API charges.

Requirements:
  - Claude Max or Pro subscription ($100-200/month)
  - Claude Code CLI installed

Configuration:
  Enable in .infer/config.yaml:
    claude_code:
      enabled: true
    gateway:
      run: false`,
}

var claudeCodeSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up Claude authentication token",
	Long: `Set up a long-lived authentication token for Claude Max/Pro subscription.

This command runs 'claude setup-token' which will guide you through the
authentication process.

Example:
  infer claude-code setup`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return err
		}
		claudePath := cfg.ClaudeCode.CLIPath

		if _, err := exec.LookPath(claudePath); err != nil {
			return fmt.Errorf("claude Code CLI not found at '%s'.\n\nMake sure Claude Code CLI is installed and in your PATH,\nor set custom path in .infer/config.yaml:\n  claude_code:\n    cli_path: /path/to/claude", claudePath)
		}

		fmt.Println("Setting up Claude authentication token...")
		fmt.Println("This requires a Claude Max or Pro subscription.")

		setupCmd := exec.Command(claudePath, "setup-token")
		setupCmd.Stdout = os.Stdout
		setupCmd.Stderr = os.Stderr
		setupCmd.Stdin = os.Stdin

		if err := setupCmd.Run(); err != nil {
			return fmt.Errorf("failed to setup token: %w", err)
		}

		fmt.Println("\n✓ Authentication setup complete!")
		fmt.Println("\nNext steps:")
		fmt.Println("1. Enable Claude Code mode in .infer/config.yaml:")
		fmt.Println("     claude_code:")
		fmt.Println("       enabled: true")
		fmt.Println("     gateway:")
		fmt.Println("       run: false")
		fmt.Println("\n2. Use infer normally:")
		fmt.Println("     infer chat")
		fmt.Println("     infer agent \"your task\"")

		return nil
	},
}

var claudeCodeTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test Claude Code CLI integration",
	Long:  `Test if Claude Code CLI is properly configured and authenticated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return err
		}
		claudePath := cfg.ClaudeCode.CLIPath

		if _, err := exec.LookPath(claudePath); err != nil {
			fmt.Printf("✗ Claude Code CLI not found at '%s'\n", claudePath)
			return fmt.Errorf("claude CLI not in PATH")
		}
		fmt.Printf("✓ Claude Code CLI found at: %s\n", claudePath)

		versionCmd := exec.Command(claudePath, "--version")
		output, err := versionCmd.Output()
		if err != nil {
			fmt.Println("✗ Could not get CLI version")
			return fmt.Errorf("failed to get version: %w", err)
		}
		fmt.Printf("✓ Version: %s\n", string(output))

		fmt.Println("\n✓ Testing authentication with a simple request...")
		testCmd := exec.Command(claudePath, "--print", "--model", "claude-3-5-haiku-20241022", "respond with just the word 'success'")
		testOutput, err := testCmd.CombinedOutput()
		if err != nil {
			fmt.Println("✗ Authentication test failed")
			fmt.Println("\nThis likely means you need to set up authentication.")
			fmt.Println("Run: infer claude-code setup")
			return fmt.Errorf("test request failed: %w\nOutput: %s", err, string(testOutput))
		}

		fmt.Println("✓ Authentication working!")
		fmt.Println("\nClaude Code is properly configured and authenticated.")
		fmt.Println("You can now enable it in .infer/config.yaml")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(claudeCodeCmd)
	claudeCodeCmd.AddCommand(claudeCodeSetupCmd, claudeCodeTestCmd)
}
