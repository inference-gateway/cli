package components

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInitWizard_RendersTitleAndConfirm(t *testing.T) {
	v := NewInitGithubActionView(createMockStyleProviderForHelpBar())
	v.Init() // the chat app initializes the form when the wizard is shown
	out := v.View().Content
	if !strings.Contains(out, "Init GitHub Action Setup Wizard") {
		t.Fatalf("expected wizard title in view, got:\n%s", out)
	}
	if !strings.Contains(out, "GitHub App") {
		t.Fatalf("expected confirm question in view, got:\n%s", out)
	}
}

func TestInitWizard_CtrlCCancels(t *testing.T) {
	v := NewInitGithubActionView(createMockStyleProviderForHelpBar())

	model, _ := v.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	v = model.(*InitGithubActionView)

	if !v.IsCancelled() || !v.IsDone() {
		t.Fatalf("expected ctrl+c to cancel the wizard, got cancelled=%v done=%v", v.IsCancelled(), v.IsDone())
	}
}

func TestInitWizard_ResetClearsState(t *testing.T) {
	v := NewInitGithubActionView(createMockStyleProviderForHelpBar())
	v.done = true
	v.cancelled = true
	v.phase = phaseDetails
	v.appID = "12345"
	v.privateKeyPath = "/tmp/key.pem"
	v.pemCandidates = []pemCandidate{{Path: "/tmp/key.pem"}}
	v.keyChoice = keyChoiceManual
	v.manualKeyPath = "/tmp/key.pem"
	v.browsedKeyPath = "/tmp/key.pem"
	v.browserOpened = true

	v.Reset()

	if v.done || v.cancelled {
		t.Fatalf("expected clean state after Reset, got done=%v cancelled=%v", v.done, v.cancelled)
	}
	if v.phase != phaseConfirm {
		t.Fatalf("expected phaseConfirm after Reset, got %v", v.phase)
	}
	if v.appID != "" || v.privateKeyPath != "" || v.browserOpened {
		t.Fatalf("expected results cleared after Reset, got appID=%q key=%q browser=%v", v.appID, v.privateKeyPath, v.browserOpened)
	}
	if v.pemCandidates != nil || v.keyChoice != "" || v.manualKeyPath != "" || v.browsedKeyPath != "" {
		t.Fatalf("expected key-selection state cleared after Reset")
	}
	if v.form == nil {
		t.Fatal("expected a rebuilt form after Reset")
	}
}

func TestInitWizard_GithubURLs(t *testing.T) {
	v := NewInitGithubActionView(createMockStyleProviderForHelpBar())

	if got := v.getGithubActionsURL(); got != "https://github.com/settings/apps" {
		t.Fatalf("expected personal apps URL, got %q", got)
	}

	v.SetRepositoryInfo("acme", true)
	if got := v.getGithubActionsURL(); got != "https://github.com/organizations/acme/settings/apps" {
		t.Fatalf("expected org apps URL, got %q", got)
	}

	install := v.GetInstallationURL("acme", "infra")
	if !strings.Contains(install, "acme") || !strings.Contains(install, "infra") {
		t.Fatalf("expected installation URL to include owner and repo, got %q", install)
	}
}

func TestResolvePrivateKeyPath(t *testing.T) {
	v := NewInitGithubActionView(createMockStyleProviderForHelpBar())

	v.keyChoice = "/tmp/scanned.pem"
	v.resolvePrivateKeyPath()
	if v.privateKeyPath != "/tmp/scanned.pem" {
		t.Errorf("candidate choice: got %q", v.privateKeyPath)
	}

	v.keyChoice = keyChoiceManual
	v.manualKeyPath = "/tmp/manual.pem"
	v.resolvePrivateKeyPath()
	if v.privateKeyPath != "/tmp/manual.pem" {
		t.Errorf("manual choice: got %q", v.privateKeyPath)
	}

	v.keyChoice = keyChoiceBrowse
	v.browsedKeyPath = "/tmp/browsed.pem"
	v.resolvePrivateKeyPath()
	if v.privateKeyPath != "/tmp/browsed.pem" {
		t.Errorf("browse choice: got %q", v.privateKeyPath)
	}
}

func TestValidatePemPath(t *testing.T) {
	dir := t.TempDir()
	pem := dir + "/key.pem"
	if err := os.WriteFile(pem, []byte("key"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := validatePemPath(pem); err != nil {
		t.Errorf("existing .pem: unexpected error %v", err)
	}
	if err := validatePemPath(""); err == nil {
		t.Error("empty path: expected error")
	}
	if err := validatePemPath(dir + "/missing.pem"); err == nil {
		t.Error("missing file: expected error")
	}
	if err := validatePemPath(pem + ".txt"); err == nil {
		t.Error("wrong extension: expected error")
	}
}

func TestValidateAppID(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"", false},
		{"12345", true},
		{"12a45", false},
		{"abc", false},
	}
	for _, c := range cases {
		err := validateAppID(c.in)
		if c.valid && err != nil {
			t.Errorf("validateAppID(%q): expected valid, got %v", c.in, err)
		}
		if !c.valid && err == nil {
			t.Errorf("validateAppID(%q): expected error, got nil", c.in)
		}
	}
}
