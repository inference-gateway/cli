package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var setupGithubAppCmd = &cobra.Command{
	Use:   "setup-github-app",
	Short: "Setup GitHub App for infer-action bot identity",
	Long: `Interactive wizard to setup a GitHub App for use with infer-action.
This creates a custom bot identity (e.g., "infer[bot]") for commits and PRs
instead of using the default "github-actions[bot]".

The wizard will:
1. Guide you through creating a GitHub App
2. Help you configure the required permissions
3. Add secrets to your repository
4. Generate a workflow file with app token support`,
	RunE: runSetupGithubApp,
}

func init() {
	rootCmd.AddCommand(setupGithubAppCmd)
}

// Wizard logic is inherently sequential and keeping it together improves readability
func runSetupGithubApp(cmd *cobra.Command, args []string) error { //nolint:funlen
	fmt.Println("🤖 GitHub App Setup Wizard for infer-action")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println()

	repo, err := validateEnvironment()
	if err != nil {
		return err
	}

	fmt.Printf("Repository: %s\n", repo)

	parts := strings.Split(repo, "/")
	owner := parts[0]
	isOrg := isOrgRepo(repo)

	appIDExists := checkSecretExists(repo, "INFER_APP_ID")
	privateKeyExists := checkSecretExists(repo, "INFER_APP_PRIVATE_KEY")

	if appIDExists && privateKeyExists {
		fmt.Println()
		fmt.Println("✅ GitHub App secrets already configured for this repository!")
		fmt.Println()
		if !promptYesNo("Do you want to reconfigure them?") {
			fmt.Println("Keeping existing configuration.")
			return nil
		}
	}

	fmt.Println()
	fmt.Println("Do you already have a GitHub App for infer-action?")
	fmt.Println()
	fmt.Println("💡 Tip: You can reuse one GitHub App across multiple repositories!")
	fmt.Println("   If you've already created an app, select 'yes' to reuse it.")
	fmt.Println()

	var appID, privateKey string
	hasExistingApp := promptYesNo("Do you have an existing GitHub App you want to use?")

	if hasExistingApp {
		appID, privateKey, err = setupExistingApp(repo, owner, isOrg)
		if err != nil {
			return err
		}
	} else {
		appID, privateKey, err = setupNewApp(owner, isOrg)
		if err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Println("Step 2: Configure Secrets")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()
	fmt.Println("Adding secrets to repository...")

	if err := setGhSecret(repo, "INFER_APP_ID", appID); err != nil {
		return fmt.Errorf("failed to set INFER_APP_ID secret: %w", err)
	}
	fmt.Println("✓ Added INFER_APP_ID secret")

	if err := setGhSecret(repo, "INFER_APP_PRIVATE_KEY", privateKey); err != nil {
		return fmt.Errorf("failed to set INFER_APP_PRIVATE_KEY secret: %w", err)
	}
	fmt.Println("✓ Added INFER_APP_PRIVATE_KEY secret")

	fmt.Println()
	fmt.Println("Step 3: Generate Workflow")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()

	workflowPath := ".github/workflows/infer.yml"
	workflowExists := fileExists(workflowPath)

	if workflowExists {
		fmt.Printf("⚠️  Workflow file already exists: %s\n", workflowPath)
		if !promptYesNo("Do you want to update it with GitHub App support?") {
			fmt.Println("Skipping workflow update.")
			fmt.Println("\n✅ Setup complete! Secrets have been added.")
			fmt.Println("\n⚠️  Remember to manually update your workflow to use the GitHub App token.")
			return nil
		}
	}

	workflow := generateWorkflow()

	if err := os.MkdirAll(".github/workflows", 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(workflowPath, []byte(workflow), 0644); err != nil {
		return fmt.Errorf("failed to write workflow file: %w", err)
	}
	fmt.Printf("✓ Created/updated workflow file: %s\n", workflowPath)

	fmt.Println()
	fmt.Println("Step 4: Create Pull Request")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()

	if !promptYesNo("Create a pull request with these changes?") {
		fmt.Println("\n✅ Setup complete!")
		fmt.Println("\nManual steps:")
		fmt.Printf("  1. Review changes: git diff %s\n", workflowPath)
		fmt.Println("  2. Commit: git add . && git commit -m 'feat(ci): Setup GitHub App for infer-action bot'")
		fmt.Println("  3. Push: git push")
		return nil
	}

	if err := createSetupPR(workflowPath); err != nil {
		fmt.Printf("⚠️  Failed to create PR: %v\n", err)
		fmt.Println("\nYou can manually create a PR with:")
		fmt.Printf("  git checkout -b ci/setup-infer-app\n")
		fmt.Printf("  git add %s\n", workflowPath)
		fmt.Println("  git commit -m 'feat(ci): Setup GitHub App for infer-action bot'")
		fmt.Println("  git push -u origin ci/setup-infer-app")
		fmt.Println("  gh pr create --title 'feat(ci): Setup GitHub App for infer-action bot' --body 'Sets up custom GitHub App for infer-action bot identity'")
		return err
	}

	fmt.Println()
	fmt.Println("✅ Setup complete!")
	fmt.Println("📝 Review and merge the pull request to activate the infer bot.")

	return nil
}

func isGhCliInstalled() bool {
	cmd := exec.Command("gh", "version")
	return cmd.Run() == nil
}

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func validateEnvironment() (string, error) {
	if !isGhCliInstalled() {
		return "", fmt.Errorf("gh CLI is not installed. Please install it from https://cli.github.com/")
	}

	if !isGitRepo() {
		return "", fmt.Errorf("not in a git repository. Please run this command from your repository root")
	}

	repo, err := getCurrentRepo()
	if err != nil {
		return "", fmt.Errorf("failed to get repository info: %w", err)
	}

	return repo, nil
}

func getCurrentRepo() (string, error) {
	cmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func isOrgRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	owner := parts[0]
	cmd := exec.Command("gh", "api", fmt.Sprintf("/orgs/%s", owner))
	return cmd.Run() == nil
}

func promptYesNo(question string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s (y/n): ", question)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))
		if response == "y" || response == "yes" {
			return true
		}
		if response == "n" || response == "no" {
			return false
		}
		fmt.Println("Please answer y or n")
	}
}

func promptInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func setGhSecret(repo, name, value string) error {
	cmd := exec.Command("gh", "secret", "set", name, "-R", repo, "--body", value)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func generateWorkflow() string {
	return `---
name: Infer

on:
  issues:
    types:
      - opened
      - edited
  issue_comment:
    types:
      - created

permissions:
  issues: write
  contents: write
  pull-requests: write

jobs:
  infer:
    runs-on: ubuntu-24.04
    steps:
      - name: Generate GitHub App Token
        uses: actions/create-github-app-token@v2.1.1
        id: app-token
        with:
          app-id: ${{ secrets.INFER_APP_ID }}
          private-key: ${{ secrets.INFER_APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: |
            ${{ github.event.repository.name }}

      - name: Checkout repository
        uses: actions/checkout@v5
        with:
          token: ${{ steps.app-token.outputs.token }}

      - name: Run Infer Agent
        uses: inference-gateway/infer-action@v0.3.1
        with:
          github-token: ${{ steps.app-token.outputs.token }}
          trigger-phrase: "@infer"
          model: deepseek/deepseek-chat
          max-turns: 50
          anthropic-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          google-api-key: ${{ secrets.GOOGLE_API_KEY }}
          deepseek-api-key: ${{ secrets.DEEPSEEK_API_KEY }}
          groq-api-key: ${{ secrets.GROQ_API_KEY }}
          mistral-api-key: ${{ secrets.MISTRAL_API_KEY }}
          cloudflare-api-key: ${{ secrets.CLOUDFLARE_API_KEY }}
          cohere-api-key: ${{ secrets.COHERE_API_KEY }}
          ollama-api-key: ${{ secrets.OLLAMA_API_KEY }}
          ollama-cloud-api-key: ${{ secrets.OLLAMA_CLOUD_API_KEY }}
`
}

func createSetupPR(workflowPath string) error {
	cmd := exec.Command("git", "branch", "--show-current")
	currentBranch, err := cmd.Output()
	if err != nil {
		return err
	}
	branch := strings.TrimSpace(string(currentBranch))

	if branch == "main" || branch == "master" {
		branch = "ci/setup-infer-app"
		fmt.Printf("Creating branch: %s\n", branch)
		cmd = exec.Command("git", "checkout", "-b", branch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	cmd = exec.Command("git", "add", workflowPath)
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("git", "commit", "-m", "feat(ci): Setup GitHub App for infer-action bot identity")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("git", "push", "-u", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	prBody := "## Summary\n\n" +
		"This PR sets up a custom GitHub App for the infer-action bot, allowing commits and PRs to appear as \"infer[bot]\" instead of \"github-actions[bot]\".\n\n" +
		"## Changes\n\n" +
		"- Added GitHub App token generation step using `actions/create-github-app-token@v2`\n" +
		"- Updated workflow to use app token instead of default GITHUB_TOKEN\n" +
		"- Configured secrets: `INFER_APP_ID` and `INFER_APP_PRIVATE_KEY`\n\n" +
		"## Testing\n\n" +
		"After merging, @infer mentions in issues should create commits/PRs with the custom bot identity.\n\n" +
		"🤖 Generated with infer setup-github-app\n"
	cmd = exec.Command("gh", "pr", "create",
		"--title", "feat(ci): Setup GitHub App for infer-action bot identity",
		"--body", prBody,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func setupExistingApp(repo, owner string, isOrg bool) (appID, privateKey string, err error) {
	fmt.Println()
	fmt.Println("🔍 Fetching your GitHub Apps...")
	fmt.Println()

	apps, err := listUserGitHubApps()
	if err != nil {
		fmt.Printf("⚠️  Could not fetch GitHub Apps automatically: %v\n", err)
		fmt.Println("Falling back to manual input...")
		return setupExistingAppManual(owner, isOrg)
	}

	if len(apps) == 0 {
		fmt.Println("❌ No GitHub Apps found.")
		fmt.Println()
		if promptYesNo("Would you like to create a new app instead?") {
			return setupNewApp(owner, isOrg)
		}
		return "", "", fmt.Errorf("no GitHub Apps available")
	}

	options := make([]string, len(apps))
	for i, app := range apps {
		accountInfo := fmt.Sprintf("(%s/%s)", app.Account.Type, app.Account.Login)
		options[i] = fmt.Sprintf("%s %s - App ID: %d", app.AppSlug, accountInfo, app.AppID)
	}

	fmt.Println("Select a GitHub App to use:")
	selectedIndex, err := promptSelection("", options)
	if err != nil {
		return "", "", fmt.Errorf("failed to get selection: %w", err)
	}

	selectedApp := apps[selectedIndex]
	fmt.Printf("\n✅ Selected: %s (App ID: %d)\n", selectedApp.AppSlug, selectedApp.AppID)
	fmt.Println()

	fmt.Println("🔍 Checking if app is installed on this repository...")
	installed, err := checkAppInstalledOnRepo(selectedApp.ID, repo)
	if err != nil {
		fmt.Printf("⚠️  Could not verify installation: %v\n", err)
		fmt.Println()
	}

	// Installation flow requires checking multiple user confirmations
	if !installed { //nolint:nestif
		fmt.Println("⚠️  This app is not installed on the current repository.")
		fmt.Println()
		installURL := getAppInstallationURL(owner, isOrg)
		fmt.Printf("Please install it at: %s\n", installURL)
		fmt.Println()

		if promptYesNo("Open installation page in browser?") {
			if err := openBrowser(installURL); err != nil {
				fmt.Printf("⚠️  Failed to open browser: %v\n", err)
			}
			fmt.Println()
		}

		if !promptYesNo("Have you installed the app on this repository?") {
			return "", "", fmt.Errorf("app must be installed on repository before continuing")
		}
	} else {
		fmt.Println("✅ App is already installed on this repository!")
		fmt.Println()
	}

	fmt.Println("📝 Private Key Required")
	fmt.Println()
	fmt.Println("Note: GitHub doesn't allow retrieving existing private keys for security reasons.")
	fmt.Println("You can either:")
	fmt.Println("  1. Use your existing private key file (.pem)")
	fmt.Println("  2. Generate a new private key for this app")
	fmt.Println()

	privateKeyPath := promptInput("Enter path to your private key (.pem file): ")
	if privateKeyPath == "" {
		return "", "", fmt.Errorf("private key path is required")
	}

	if strings.HasPrefix(privateKeyPath, "~") {
		home, _ := os.UserHomeDir()
		privateKeyPath = strings.Replace(privateKeyPath, "~", home, 1)
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read private key file: %w", err)
	}
	privateKey = string(privateKeyBytes)

	appID = strconv.FormatInt(selectedApp.AppID, 10)
	return appID, privateKey, nil
}

func setupExistingAppManual(owner string, isOrg bool) (appID, privateKey string, err error) {
	fmt.Println()
	fmt.Println("Setting up existing GitHub App (Manual Mode)")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()

	fmt.Println("💡 Tip: You can reuse a GitHub App across multiple repositories!")
	fmt.Println()

	installURL := getAppInstallationURL(owner, isOrg)
	fmt.Println("Make sure your GitHub App is installed on this repository.")
	fmt.Printf("Visit: %s\n", installURL)
	fmt.Println()

	if promptYesNo("Open installation page in browser?") {
		if err := openBrowser(installURL); err != nil {
			fmt.Printf("⚠️  Failed to open browser: %v\n", err)
		}
		fmt.Println()
	}

	if !promptYesNo("Is the app installed on this repository?") {
		return "", "", fmt.Errorf("app must be installed on repository before continuing")
	}

	fmt.Println()
	appID = promptInput("Enter your GitHub App ID: ")
	if appID == "" {
		return "", "", fmt.Errorf("app ID is required")
	}

	privateKeyPath := promptInput("Enter path to your private key (.pem file): ")
	if privateKeyPath == "" {
		return "", "", fmt.Errorf("private key path is required")
	}

	if strings.HasPrefix(privateKeyPath, "~") {
		home, _ := os.UserHomeDir()
		privateKeyPath = strings.Replace(privateKeyPath, "~", home, 1)
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read private key file: %w", err)
	}
	privateKey = string(privateKeyBytes)

	return appID, privateKey, nil
}

func setupNewApp(owner string, isOrg bool) (appID, privateKey string, err error) {
	fmt.Println()
	fmt.Println("Step 1: Create GitHub App")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println()
	fmt.Println("You need to create a GitHub App with the following settings:")
	fmt.Println()
	fmt.Println("📝 Recommended App Name: infer")
	fmt.Println("🏠 Homepage URL: https://github.com/inference-gateway/infer-action")
	fmt.Println("📖 Description: AI-powered coding assistant that responds to @infer mentions")
	fmt.Println()
	fmt.Println("Required Permissions (Repository permissions):")
	fmt.Println("  • Contents: Read and write")
	fmt.Println("  • Issues: Read and write")
	fmt.Println("  • Pull requests: Read and write")
	fmt.Println("  • Metadata: Read-only (automatically selected)")
	fmt.Println()
	fmt.Println("❌ Webhook: Uncheck 'Active' (not needed)")
	fmt.Println("🔒 Where can this GitHub App be installed?: 'Only on this account'")
	fmt.Println()

	if !promptYesNo("Ready to open GitHub App creation page in your browser?") {
		return "", "", fmt.Errorf("cancelled by user")
	}

	appCreationURL := "https://github.com/settings/apps/new"
	if isOrg {
		appCreationURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new", owner)
	}

	fmt.Printf("\nOpening: %s\n\n", appCreationURL)
	if err := openBrowser(appCreationURL); err != nil {
		fmt.Printf("⚠️  Failed to open browser: %v\n", err)
		fmt.Printf("Please manually visit: %s\n\n", appCreationURL)
	}

	fmt.Println("After creating the app:")
	fmt.Println("  1. Note the App ID (shown at the top)")
	fmt.Println("  2. Scroll down and click 'Generate a private key'")
	fmt.Println("  3. Save the downloaded .pem file")
	fmt.Println("  4. Install the app on this repository")
	fmt.Println()

	if !promptYesNo("Have you completed the app creation and installation?") {
		return "", "", fmt.Errorf("setup incomplete - run this command again when ready")
	}

	fmt.Println()
	appID = promptInput("Enter your GitHub App ID: ")
	if appID == "" {
		return "", "", fmt.Errorf("app ID is required")
	}

	privateKeyPath := promptInput("Enter path to your private key (.pem file): ")
	if privateKeyPath == "" {
		return "", "", fmt.Errorf("private key path is required")
	}

	if strings.HasPrefix(privateKeyPath, "~") {
		home, _ := os.UserHomeDir()
		privateKeyPath = strings.Replace(privateKeyPath, "~", home, 1)
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read private key file: %w", err)
	}
	privateKey = string(privateKeyBytes)

	return appID, privateKey, nil
}

func checkSecretExists(repo, secretName string) bool {
	cmd := exec.Command("gh", "secret", "list", "-R", repo)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), secretName)
}

type GitHubApp struct {
	ID         int64   `json:"id"`
	AppID      int64   `json:"app_id"`
	AppSlug    string  `json:"app_slug"`
	TargetType string  `json:"target_type"`
	Account    Account `json:"account"`
}

type Account struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type InstallationsResponse struct {
	Installations []GitHubApp `json:"installations"`
}

func listUserGitHubApps() ([]GitHubApp, error) {
	cmd := exec.Command("gh", "api", "/user/installations", "--paginate")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub Apps: %w", err)
	}

	var response InstallationsResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub Apps response: %w", err)
	}

	return response.Installations, nil
}

func checkAppInstalledOnRepo(installationID int64, repo string) (bool, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("/user/installations/%d/repositories", installationID))
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	return strings.Contains(string(output), repo), nil
}

func promptSelection(prompt string, options []string) (int, error) {
	fmt.Println(prompt)
	fmt.Println()

	for i, option := range options {
		fmt.Printf("  %d. %s\n", i+1, option)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter selection (1-" + strconv.Itoa(len(options)) + "): ")
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(response)

		selection, err := strconv.Atoi(response)
		if err != nil || selection < 1 || selection > len(options) {
			fmt.Printf("Invalid selection. Please enter a number between 1 and %d\n", len(options))
			continue
		}

		return selection - 1, nil
	}
}

func getAppInstallationURL(owner string, isOrg bool) string {
	if isOrg {
		return fmt.Sprintf("https://github.com/organizations/%s/settings/installations", owner)
	}
	return "https://github.com/settings/installations"
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
