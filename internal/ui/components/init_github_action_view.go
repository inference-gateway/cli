package components

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// wizardPhase identifies which huh form is currently driving the wizard.
type wizardPhase int

const (
	phaseConfirm wizardPhase = iota
	phaseDetails
)

// InitGithubActionView drives the GitHub App setup wizard with huh forms. The
// flow is split across two forms so that the browser can be opened between the
// "create a new app?" answer and the App ID prompt: confirm -> (open browser
// when creating new) -> App ID -> private key (skipped when org secrets already
// exist for that App ID).
type InitGithubActionView struct {
	width         int
	height        int
	styleProvider *styles.Provider
	done          bool
	cancelled     bool

	phase         wizardPhase
	form          *huh.Form
	browserOpened bool

	// Form-bound results.
	hasExisting    bool
	appID          string
	privateKeyPath string
	err            error

	// Repository information.
	repoOwner string
	isOrgRepo bool

	// Callback to check if org secrets already exist for an App ID.
	checkSecretsExist func(appID string) bool
}

// NewInitGithubActionView creates a new GitHub App setup wizard.
func NewInitGithubActionView(styleProvider *styles.Provider) *InitGithubActionView {
	v := &InitGithubActionView{
		width:         80,
		height:        24,
		styleProvider: styleProvider,
		phase:         phaseConfirm,
	}
	v.form = v.buildConfirmForm()
	return v
}

func (v *InitGithubActionView) buildConfirmForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("hasExisting").
				Title("Do you already have a GitHub App for infer-action?").
				Description("Tip: you can reuse one GitHub App across multiple repositories.").
				Affirmative("Yes, I have one").
				Negative("No, create a new one").
				Value(&v.hasExisting),
		),
	).WithShowHelp(true).WithWidth(v.width)
}

func (v *InitGithubActionView) buildDetailsForm() *huh.Form {
	appIDInput := huh.NewInput().
		Key("appID").
		Title("GitHub App ID").
		Description("Find it on your app's page: " + v.getGithubActionsURL()).
		Validate(validateAppID).
		Value(&v.appID)

	keyPicker := huh.NewFilePicker().
		Key("privateKey").
		Title("Private key (.pem)").
		Description("Generate one under your app's 'Private keys' section, then pick the file.").
		CurrentDirectory(".").
		AllowedTypes([]string{".pem"}).
		ShowHidden(false).
		Height(10).
		Value(&v.privateKeyPath)

	return huh.NewForm(
		huh.NewGroup(appIDInput),
		huh.NewGroup(keyPicker).WithHideFunc(func() bool {
			return v.checkSecretsExist != nil && v.checkSecretsExist(v.appID)
		}),
	).WithShowHelp(true).WithWidth(v.width)
}

func (v *InitGithubActionView) Init() tea.Cmd {
	return v.form.Init()
}

func (v *InitGithubActionView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		v.width = wsm.Width
		v.height = wsm.Height
	}

	form, cmd := v.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		v.form = f
	}

	switch v.form.State {
	case huh.StateCompleted:
		return v, v.advance()
	case huh.StateAborted:
		v.cancelled = true
		v.done = true
		return v, nil
	default:
		return v, cmd
	}
}

// advance moves the wizard to the next phase when a form completes. After the
// confirm form it opens the GitHub App creation page in the browser (only when
// the user is creating a new app) and switches to the details form; after the
// details form the wizard is done.
func (v *InitGithubActionView) advance() tea.Cmd {
	if v.phase == phaseConfirm {
		if !v.hasExisting && !v.browserOpened {
			_ = openGithubActionCreationURL(v.repoOwner, v.isOrgRepo)
			v.browserOpened = true
		}
		v.phase = phaseDetails
		v.form = v.buildDetailsForm()
		return v.form.Init()
	}

	v.done = true
	return nil
}

func (v *InitGithubActionView) View() tea.View {
	if v.done {
		if v.cancelled {
			return tea.NewView("Setup cancelled.\n")
		}
		return tea.NewView("Setup complete!\n")
	}

	var b strings.Builder
	b.WriteString(v.styleProvider.RenderWithColor("Init GitHub Action Setup Wizard", v.styleProvider.GetThemeColor("accent")))
	b.WriteString("\n\n")
	b.WriteString(v.form.View())
	return tea.NewView(b.String())
}

// validateAppID requires a non-empty, all-numeric App ID, mirroring the
// numeric-only input sanitisation of the previous wizard.
func validateAppID(s string) error {
	if s == "" {
		return fmt.Errorf("app ID is required")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return fmt.Errorf("app ID must be numeric")
		}
	}
	return nil
}

// IsDone returns whether the wizard is complete.
func (v *InitGithubActionView) IsDone() bool {
	return v.done
}

// IsCancelled returns whether the wizard was cancelled.
func (v *InitGithubActionView) IsCancelled() bool {
	return v.cancelled
}

// GetResult returns the wizard result.
func (v *InitGithubActionView) GetResult() (appID, privateKeyPath string, err error) {
	return v.appID, v.privateKeyPath, v.err
}

// SetWidth sets the width of the view.
func (v *InitGithubActionView) SetWidth(width int) {
	v.width = width
}

// SetHeight sets the height of the view.
func (v *InitGithubActionView) SetHeight(height int) {
	v.height = height
}

// SetSecretsExistChecker sets the callback to check if org secrets exist.
func (v *InitGithubActionView) SetSecretsExistChecker(checker func(appID string) bool) {
	v.checkSecretsExist = checker
}

// SetRepositoryInfo sets the repository owner and whether it's an org.
func (v *InitGithubActionView) SetRepositoryInfo(owner string, isOrg bool) {
	v.repoOwner = owner
	v.isOrgRepo = isOrg
}

// getGithubActionsURL returns the appropriate GitHub Apps URL based on whether this is an org repo
func (v *InitGithubActionView) getGithubActionsURL() string {
	if v.isOrgRepo && v.repoOwner != "" {
		return fmt.Sprintf("https://github.com/organizations/%s/settings/apps", v.repoOwner)
	}
	return "https://github.com/settings/apps"
}

// Reset resets the view state for reuse.
func (v *InitGithubActionView) Reset() {
	v.done = false
	v.cancelled = false
	v.phase = phaseConfirm
	v.hasExisting = false
	v.appID = ""
	v.privateKeyPath = ""
	v.browserOpened = false
	v.err = nil
	v.form = v.buildConfirmForm()
}

// openGithubActionCreationURL opens the GitHub App creation page with pre-filled parameters
func openGithubActionCreationURL(owner string, isOrg bool) error {
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

// GetInstallationURL returns the URL to install the GitHub App on a repository
func (v *InitGithubActionView) GetInstallationURL(repoOwner, repoName string) string {
	if repoOwner != "" && repoName != "" {
		return fmt.Sprintf("https://github.com/apps/infer-bot/installations/new/permissions?target_id=%s&repository=%s", repoOwner, repoName)
	}
	return "https://github.com/apps/infer-bot/installations/new"
}
