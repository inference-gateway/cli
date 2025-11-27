package components

import (
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// GitHubAppSetupView implements the GitHub App setup wizard UI
type GitHubAppSetupView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	done          bool
	cancelled     bool

	// Wizard state
	step           int // 0=introduction, 1=ask existing, 2=enter app ID manually, 3=private key input, 4=installation
	hasExisting    bool
	privateKeyPath string
	inputBuffer    string
	cursorPos      int

	// File selection
	showingFilePicker bool
	filePickerFiles   []string
	filePickerIndex   int

	// Results
	appID      string
	privateKey string
	err        error

	// Repository information
	repoOwner string
	isOrgRepo bool

	// Callback to check if org secrets already exist
	checkSecretsExist func(appID string) bool
}

// NewGitHubAppSetupView creates a new GitHub App setup wizard
func NewGitHubAppSetupView(styleProvider *styles.Provider) *GitHubAppSetupView {
	return &GitHubAppSetupView{
		width:         80,
		height:        24,
		styleProvider: styleProvider,
		step:          0,
	}
}

func (v *GitHubAppSetupView) Init() tea.Cmd {
	return nil
}

func (v *GitHubAppSetupView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		return v, nil

	case tea.KeyMsg:
		return v.handleKeyInput(msg)
	}

	return v, nil
}

func (v *GitHubAppSetupView) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if v.showingFilePicker {
		return v.handleFilePickerKeys(msg)
	}

	switch msg.String() {
	case "ctrl+c", "esc":
		return v.handleCancel()
	case "enter":
		return v.handleEnter()
	case "tab", "ctrl+f":
		return v.handleFilePicker()
	case "y", "Y":
		return v.handleYesInput()
	case "n", "N":
		return v.handleNoInput()
	case "left":
		return v.handleLeftArrow()
	case "right":
		return v.handleRightArrow()
	case "backspace", "delete":
		return v.handleBackspace()
	default:
		return v.handleTextInput(msg)
	}
}

func (v *GitHubAppSetupView) handleCancel() (tea.Model, tea.Cmd) {
	v.cancelled = true
	v.done = true
	return v, nil
}

func (v *GitHubAppSetupView) handleFilePicker() (tea.Model, tea.Cmd) {
	if v.step == 3 {
		v.showingFilePicker = true
		v.loadPemFiles()
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleYesInput() (tea.Model, tea.Cmd) {
	if v.step == 1 {
		v.hasExisting = true
		v.step = 2
		v.inputBuffer = ""
		v.cursorPos = 0
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleNoInput() (tea.Model, tea.Cmd) {
	if v.step == 1 {
		v.hasExisting = false
		_ = openGitHubAppCreationURL(v.repoOwner, v.isOrgRepo)
		v.step = 2
		v.inputBuffer = ""
		v.cursorPos = 0
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleLeftArrow() (tea.Model, tea.Cmd) {
	if (v.step == 2 || v.step == 3) && v.cursorPos > 0 {
		v.cursorPos--
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleRightArrow() (tea.Model, tea.Cmd) {
	if (v.step == 2 || v.step == 3) && v.cursorPos < len(v.inputBuffer) {
		v.cursorPos++
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleBackspace() (tea.Model, tea.Cmd) {
	if (v.step == 2 || v.step == 3) && v.cursorPos > 0 {
		v.inputBuffer = v.inputBuffer[:v.cursorPos-1] + v.inputBuffer[v.cursorPos:]
		v.cursorPos--
	}
	return v, nil
}

func (v *GitHubAppSetupView) handleTextInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if v.step != 2 && v.step != 3 {
		return v, nil
	}

	if msg.Type == tea.KeyRunes {
		input := v.sanitizeInput(string(msg.Runes))
		if len(input) > 0 {
			v.inputBuffer = v.inputBuffer[:v.cursorPos] + input + v.inputBuffer[v.cursorPos:]
			v.cursorPos += len(input)
		}
	}
	return v, nil
}

func (v *GitHubAppSetupView) sanitizeInput(input string) string {
	input = strings.ReplaceAll(input, "[", "")
	input = strings.ReplaceAll(input, "]", "")

	if v.step == 2 {
		filtered := ""
		for _, r := range input {
			if r >= '0' && r <= '9' {
				filtered += string(r)
			}
		}
		return filtered
	}

	return input
}

func (v *GitHubAppSetupView) handleFilePickerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.showingFilePicker = false
		return v, nil

	case "up", "k":
		if v.filePickerIndex > 0 {
			v.filePickerIndex--
		}
		return v, nil

	case "down", "j":
		if v.filePickerIndex < len(v.filePickerFiles)-1 {
			v.filePickerIndex++
		}
		return v, nil

	case "enter":
		if len(v.filePickerFiles) > 0 && v.filePickerIndex < len(v.filePickerFiles) {
			selectedFile := v.filePickerFiles[v.filePickerIndex]
			v.inputBuffer = selectedFile
			v.cursorPos = len(selectedFile)
			v.showingFilePicker = false
		}
		return v, nil
	}

	return v, nil
}

func (v *GitHubAppSetupView) loadPemFiles() {
	v.filePickerFiles = []string{}
	v.filePickerIndex = 0

	// Only search in current directory
	files, err := filepath.Glob("*.pem")
	if err == nil {
		v.filePickerFiles = files
	}
}

func (v *GitHubAppSetupView) handleEnter() (tea.Model, tea.Cmd) {
	switch v.step {
	case 0:
		// Introduction - move to next step
		v.step = 1
		return v, nil
	case 2:
		if len(v.inputBuffer) > 0 {
			v.appID = v.inputBuffer
			v.inputBuffer = ""
			v.cursorPos = 0

			if v.checkSecretsExist != nil && v.checkSecretsExist(v.appID) {
				v.step = 4 // Skip to installation instructions if secrets exist
			} else {
				v.step = 3
			}
		}
		return v, nil

	case 3:
		v.privateKeyPath = v.inputBuffer
		v.step = 4 // Move to installation instructions
		return v, nil
	case 4:
		// Installation instructions - complete setup
		v.done = true
		return v, nil
	}

	return v, nil
}

func (v *GitHubAppSetupView) View() string {
	if v.done {
		if v.cancelled {
			return "❌ Setup cancelled.\n"
		}
		return v.renderCompletionMessage()
	}

	var b strings.Builder

	accentColor := v.styleProvider.GetThemeColor("accent")
	b.WriteString(v.styleProvider.RenderWithColor("GitHub App Setup Wizard", accentColor))
	b.WriteString("\n\n")

	switch v.step {
	case 0:
		v.renderStepIntroduction(&b)
	case 1:
		v.renderStepAskExisting(&b)
	case 2:
		v.renderStepManualAppID(&b)
	case 3:
		v.renderStepPrivateKey(&b)
	case 4:
		v.renderStepInstallation(&b)
	}

	b.WriteString("\n\n")
	b.WriteString(v.styleProvider.RenderDimText("Press Esc to cancel"))

	return b.String()
}

func (v *GitHubAppSetupView) renderStepIntroduction(b *strings.Builder) {
	b.WriteString("🚀 GitHub App Setup Wizard\n\n")
	b.WriteString("This wizard will help you set up a GitHub App for the infer-action bot.\n\n")
	b.WriteString("What this setup does:\n")
	b.WriteString("• Creates or configures a GitHub App for automated workflows\n")
	b.WriteString("• Sets up proper permissions for code review and automation\n")
	b.WriteString("• Configures the bot identity for your repositories\n\n")
	b.WriteString("You'll need:\n")
	b.WriteString("• GitHub account with repository access\n")
	b.WriteString("• Permission to install apps on your organization/repositories\n\n")
	b.WriteString("Press Enter to continue")
}

func (v *GitHubAppSetupView) renderStepAskExisting(b *strings.Builder) {
	b.WriteString("Do you already have a GitHub App for infer-action?\n\n")
	b.WriteString("Tip: You can reuse one GitHub App across multiple repositories!\n\n")
	b.WriteString("Press Y for Yes, N for No")
}

func (v *GitHubAppSetupView) renderStepManualAppID(b *strings.Builder) {
	if v.hasExisting {
		b.WriteString("Enter your GitHub App ID\n\n")
		b.WriteString("You can find your App ID at:\n")

		accentColor := v.styleProvider.GetThemeColor("accent")
		appsURL := v.getGitHubAppsURL()
		link := v.styleProvider.RenderWithColor(appsURL, accentColor)
		b.WriteString("  " + link + "\n\n")

		b.WriteString("Click on your app name to see the App ID\n\n")
	} else {
		b.WriteString("Browser opened with pre-filled GitHub App creation form\n\n")
		b.WriteString("After creating your app, copy the App ID and paste it below.\n\n")
	}

	before := v.inputBuffer[:v.cursorPos]
	after := v.inputBuffer[v.cursorPos:]
	b.WriteString("App ID: > " + before + "_" + after + "\n\n")
	b.WriteString("Press Enter when done")
}

func (v *GitHubAppSetupView) renderStepPrivateKey(b *strings.Builder) {
	fmt.Fprintf(b, "Selected App ID: %s\n\n", v.appID)
	b.WriteString("Private Key Required\n\n")

	if v.showingFilePicker {
		v.renderFilePicker(b)
		return
	}

	b.WriteString("To download your private key:\n")
	accentColor := v.styleProvider.GetThemeColor("accent")
	appsPageURL := v.getGitHubAppsURL()
	link := v.styleProvider.RenderWithColor(appsPageURL, accentColor)
	b.WriteString("  1. Go to " + link + "\n")
	b.WriteString("  2. Click on your app name from the list\n")
	b.WriteString("  3. Scroll to 'Private keys' section\n")
	b.WriteString("  4. Click 'Generate a private key' (if you haven't already)\n")
	b.WriteString("  5. Save the downloaded .pem file\n\n")

	b.WriteString("Enter the full path to the .pem file:\n")
	b.WriteString("(e.g., /Users/yourname/Downloads/app-name.2024-01-01.private-key.pem)\n")

	before := v.inputBuffer[:v.cursorPos]
	after := v.inputBuffer[v.cursorPos:]
	b.WriteString("> " + before + "_" + after + "\n\n")

	tabHint := v.styleProvider.RenderDimText("Press Tab to browse .pem files in current directory")
	b.WriteString(tabHint + " | Press Enter when done")
}

func (v *GitHubAppSetupView) renderStepInstallation(b *strings.Builder) {
	b.WriteString("🎉 GitHub App Setup Complete!\n\n")
	
	if v.hasExisting {
		b.WriteString("Your existing GitHub App has been configured successfully.\n\n")
	} else {
		b.WriteString("Your new GitHub App has been created successfully.\n\n")
	}
	
	b.WriteString("📋 Installation Instructions:\n\n")
	
	// Generate installation URL
	installationURL := v.getGitHubAppInstallationURL()
	accentColor := v.styleProvider.GetThemeColor("accent")
	link := v.styleProvider.RenderWithColor(installationURL, accentColor)
	
	b.WriteString("1. Install the GitHub App on your repositories:\n")
	b.WriteString("   " + link + "\n\n")
	
	b.WriteString("2. Select the repositories where you want to use infer-action\n\n")
	
	b.WriteString("3. The app will be ready to use in your GitHub Actions workflows\n\n")
	
	b.WriteString("💡 Tip: You can install the app on specific repositories or all repositories\n\n")
	
	b.WriteString("Press Enter to complete setup")
}

func (v *GitHubAppSetupView) renderCompletionMessage() string {
	var b strings.Builder
	
	b.WriteString("🎉 GitHub App Setup Complete!\n\n")
	
	if v.hasExisting {
		b.WriteString("✅ Your existing GitHub App has been configured successfully!\n\n")
	} else {
		b.WriteString("✅ Your new GitHub App has been created successfully!\n\n")
	}
	
	b.WriteString("📋 Next Steps:\n\n")
	
	// Generate installation URL
	installationURL := v.getGitHubAppInstallationURL()
	accentColor := v.styleProvider.GetThemeColor("accent")
	link := v.styleProvider.RenderWithColor(installationURL, accentColor)
	
	b.WriteString("1. Install the GitHub App on your repositories:\n")
	b.WriteString("   " + link + "\n\n")
	
	b.WriteString("2. Select the repositories where you want to use infer-action\n\n")
	
	b.WriteString("3. The app will be ready to use in your GitHub Actions workflows\n\n")
	
	b.WriteString("💡 Tip: You can install the app on specific repositories or all repositories\n\n")
	
	b.WriteString("✨ Setup complete! You can now use infer-action in your workflows.")
	
	return b.String()
}

func (v *GitHubAppSetupView) renderFilePicker(b *strings.Builder) {
	b.WriteString("Select a .pem file (from current directory):\n\n")

	if len(v.filePickerFiles) == 0 {
		b.WriteString(v.styleProvider.RenderDimText("No .pem files found in current directory\n\n"))
		b.WriteString("Press Esc to go back and enter path manually")
		return
	}

	accentColor := v.styleProvider.GetThemeColor("accent")

	maxVisible := 10
	start := v.filePickerIndex
	if start > len(v.filePickerFiles)-maxVisible {
		start = len(v.filePickerFiles) - maxVisible
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < len(v.filePickerFiles) && i < start+maxVisible; i++ {
		file := v.filePickerFiles[i]
		if i == v.filePickerIndex {
			b.WriteString(v.styleProvider.RenderWithColor("> "+file, accentColor))
			b.WriteString("\n")
		} else {
			b.WriteString("  " + file + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(v.styleProvider.RenderDimText("Use ↑↓ or j/k to navigate, Enter to select, Esc to cancel"))
}

// IsDone returns whether the wizard is complete
func (v *GitHubAppSetupView) IsDone() bool {
	return v.done
}

// IsCancelled returns whether the wizard was cancelled
func (v *GitHubAppSetupView) IsCancelled() bool {
	return v.cancelled
}

// GetResult returns the wizard result
func (v *GitHubAppSetupView) GetResult() (appID, privateKeyPath string, err error) {
	return v.appID, v.privateKeyPath, v.err
}

// SetWidth sets the width of the view
func (v *GitHubAppSetupView) SetWidth(width int) {
	v.width = width
}

// SetHeight sets the height of the view
func (v *GitHubAppSetupView) SetHeight(height int) {
	v.height = height
}

// SetSecretsExistChecker sets the callback to check if org secrets exist
func (v *GitHubAppSetupView) SetSecretsExistChecker(checker func(appID string) bool) {
	v.checkSecretsExist = checker
}

// SetRepositoryInfo sets the repository owner and whether it's an org
func (v *GitHubAppSetupView) SetRepositoryInfo(owner string, isOrg bool) {
	v.repoOwner = owner
	v.isOrgRepo = isOrg
}

// getGitHubAppsURL returns the appropriate GitHub Apps URL based on whether this is an org repo
func (v *GitHubAppSetupView) getGitHubAppsURL() string {
	if v.isOrgRepo && v.repoOwner != "" {
		return fmt.Sprintf("https://github.com/organizations/%s/settings/apps", v.repoOwner)
	}
	return "https://github.com/settings/apps"
}

// getGitHubAppInstallationURL returns the appropriate GitHub App installation URL
func (v *GitHubAppSetupView) getGitHubAppInstallationURL() string {
	if v.isOrgRepo && v.repoOwner != "" {
		return fmt.Sprintf("https://github.com/organizations/%s/settings/installations", v.repoOwner)
	}
	return "https://github.com/settings/installations"
}

// Reset resets the view state for reuse
func (v *GitHubAppSetupView) Reset() {
	v.done = false
	v.cancelled = false
	v.step = 0
	v.hasExisting = false
	v.privateKeyPath = ""
	v.inputBuffer = ""
	v.cursorPos = 0
	v.appID = ""
	v.privateKey = ""
	v.err = nil
}

// openGitHubAppCreationURL opens the GitHub App creation page with pre-filled parameters
func openGitHubAppCreationURL(owner string, isOrg bool) error {
	params := url.Values{}
	params.Set("name", "infer-bot")
	params.Set("url", "https://github.com/inference-gateway/cli")
	params.Set("description", "AI-powered GitHub Actions bot for code review and automated workflows")
	params.Set("public", "false")

	params.Set("webhook_active", "false")

	params.Set("contents", "write")
	params.Set("pull_requests", "write")
	params.Set("issues", "write")
	params.Set("metadata", "read")

	var githubURL string
	if isOrg && owner != "" {
		githubURL = fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?%s", owner, params.Encode())
	} else {
		githubURL = "https://github.com/settings/apps/new?" + params.Encode()
	}

	return openBrowser(githubURL)
}

// openBrowser opens a URL in the default browser (cross-platform)
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
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}
