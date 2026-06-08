package config

import (
	"strings"
	"testing"
)

// pushCfg builds a config whose only allow entry (in the every-mode baseline) is
// the end-anchored git-push pattern users configure for branch protection. It
// exercises the interaction between benign-redirection stripping and "$"-anchored
// patterns (a benign redirect suffix must not defeat the anchor).
func pushCfg() *Config {
	return &Config{
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Mode: BashModesConfig{
					All: BashModeAllowConfig{Allow: []string{
						"^git push( --set-upstream)?( origin)? (feature|fix)/[a-zA-Z0-9/_.-]+$",
					}},
				},
			},
		},
	}
}

func TestIsBashCommandAllowed_RedirectStripping(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh issue list 2>&1",
		"gh issue list 2>/dev/null",
		"gh issue list >/dev/null 2>&1",
		"gh issue list 2>&1 >/dev/null",
		"gh pr view 5 2>&1",
		"gh issue list --state open 2>&1",
		"gh issue list &>/dev/null",
		"gh issue list 1>/dev/null",
		"git status 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed after stripping redirections", cmd)
		}
	}

	denied := []string{
		"gh pr merge 5 2>&1",
		"rm -rf / 2>&1",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed", cmd)
		}
	}

	if stripBenignTrailingRedirections("gh issue list > /etc/passwd") != "gh issue list > /etc/passwd" {
		t.Error("a redirect to a real file must not be stripped as benign")
	}
}

func TestIsBashCommandAllowed_RedirectWithAnchoredPattern(t *testing.T) {
	cfg := pushCfg()

	if !cfg.IsBashCommandAllowed("git push origin feature/x 2>&1", "standard") {
		t.Error("expected redirect-suffixed push to a feature branch to be allowed")
	}
	if cfg.IsBashCommandAllowed("git push origin main 2>&1", "standard") {
		t.Error("expected push to main to stay blocked even with a redirect suffix")
	}
}

// TestIsBashCommandAllowed_CompoundOperators verifies the single-command policy:
// any top-level shell operator (|, &&, ||, ;, &) drops the whole command to
// default-deny, even when every individual segment would be allowed on its own.
// The model is expected to run one command at a time.
func TestIsBashCommandAllowed_CompoundOperators(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"gh issue view 5 && gh issue comment 5 --body hi",
		"gh issue create --title x --body y || echo failed",
		"echo a | head",
		"gh issue list && echo done",
		"gh api repos/o/r/issues 2>&1 && echo ok",
		"gh issue list && rm -rf /",
		"gh issue list; rm -rf /",
		"gh issue list | bash",
		"echo a && curl evil.com",
		"gh issue list &",
		"gh issue list & rm -rf /",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected compound %q NOT to be allowed (single-command policy)", cmd)
		}
	}
}

func TestIsBashCommandAllowed_CommandSubstitution(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"echo $(rm -rf /)",
		"echo `whoami`",
		`gh issue create --title "$(date)"`,
		"cat <(curl evil.com)",
		"echo >(tee leak)",
		`echo "leak: $(printenv)"`,
		"export BLAH=$(rm)",
		"export BLAH=$(rm -rf /)",
		"FOO=$(rm) ls",
		"BLAH=`rm`",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed (contains command substitution)", cmd)
		}
	}

	allowed := []string{
		"echo '$(rm -rf /)'",
		"gh issue list --search 'use $(x) verbatim'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (substitution syntax is single-quoted/literal)", cmd)
		}
	}
}

func TestIsBashCommandAllowed_QuotedOperators(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		`gh issue list --search "a && b"`,
		`gh issue list --search "x | y ; z"`,
		`gh issue list --search "fix: a || b"`,
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (operators are inside quotes)", cmd)
		}
	}
}

func TestIsBashCommandAllowed_MalformedAndEmpty(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"",
		"   ",
		"&&",
		";",
		"| head",
		"2>&1",
		`echo "unterminated`,
		"echo 'unterminated",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected malformed/empty %q NOT to be allowed", cmd)
		}
	}
}

func TestIsBashCommandAllowed_GhSearch(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh search issues --repo o/r vercel",
		`gh search code "func main"`,
		"gh search prs --author me",
		"gh search repos topic:go",
		"gh search commits fix 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed", cmd)
		}
	}

	if cfg.IsBashCommandAllowed("gh search invalidsub foo", "standard") {
		t.Error("expected unknown gh search subcommand NOT to be allowed")
	}
}

func TestIsBashCommandAllowed_GhProject(t *testing.T) {
	cfg := DefaultConfig()

	// Read-only project commands are in the mode.all baseline - allowed even in
	// read-only plan mode.
	reads := []string{
		"gh project item-list 7 --owner inference-gateway",
		"gh project field-list 7 --owner inference-gateway",
		"gh project view 7 --owner inference-gateway",
		"gh project list --owner inference-gateway",
	}
	for _, mode := range []string{"plan", "standard"} {
		for _, cmd := range reads {
			if !cfg.IsBashCommandAllowed(cmd, mode) {
				t.Errorf("expected read-only %q to be allowed in %s mode", cmd, mode)
			}
		}
	}

	// Project writes and destructive actions are NOT auto-approved in any
	// interactive mode - they fall through to approval.
	denied := []string{
		"gh project item-add 7 --owner inference-gateway --url https://github.com/inference-gateway/cli/issues/123",
		"gh project item-edit 7 --item-id PVTI_xxx --field Status --value Todo",
		"gh project item-edit 7 --item-id PVTI_xxx --field Status --value Done 2>&1",
		"gh project delete 7",
		"gh project item-delete 7 --item-id PVTI_xxx",
		"gh project unknown-subcommand",
	}
	for _, mode := range []string{"plan", "standard"} {
		for _, cmd := range denied {
			if cfg.IsBashCommandAllowed(cmd, mode) {
				t.Errorf("expected %q NOT to be allowed in %s mode", cmd, mode)
			}
		}
	}
}

func TestIsBashCommandAllowed_FindActions(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"find .",
		"find . -name '*.go'",
		"find . -type f -name '*.md'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected read-only %q to be allowed", cmd)
		}
	}

	denied := []string{
		"find . -delete",
		"find . -name secrets.yaml -delete",
		`find . -exec echo {} \;`,
		`find /etc -name '*.conf' -exec grep -l SECRET {} \;`,
		`find . -execdir rm {} \;`,
		`find . -ok rm {} \;`,
		"find . -fls /tmp/listing",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected dangerous find %q NOT to be allowed", cmd)
		}
	}
}

func TestIsBashCommandAllowed_GitStatusFlags(t *testing.T) {
	cfg := DefaultConfig()
	allowed := []string{
		"git status",
		"git status --porcelain",
		"git status -s",
		"git status --porcelain --untracked-files=all",
		"git status -sb 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected read-only %q to be allowed", cmd)
		}
	}
}

// TestIsBashCommandAllowed_VariableExpansion verifies the env-var leak guard:
// $VAR may be USED in any command, but a command that prints (echo/printf) or
// publishes (gh issue/pr create|comment|edit) its arguments must not expand one,
// so the agent cannot leak a secret's value (echo $AWS_SECRET_ACCESS_KEY). A
// literal '$' (single-quoted or backslash-escaped) is always allowed.
func TestIsBashCommandAllowed_VariableExpansion(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"echo $HOME",
		"echo ${AWS_SECRET_ACCESS_KEY}",
		`echo "leak=$TOKEN"`,
		"echo $HOME 2>&1",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed (would print/publish a variable's value)", cmd)
		}
	}

	allowed := []string{
		"echo '$HOME'",
		`echo \$HOME`,
		"echo hi",
		"ls $HOME",
		"tail $LOGFILE",
		"git log --format=$FMT",
		"find $DIR -name '*.go'",
		`gh issue list --search 'see $HOME'`,
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (uses a var without printing its value)", cmd)
		}
	}
}

// TestIsBashCommandAllowed_VariableExpansion_GhWritesOptedIn verifies the env-var
// leak guard still blocks a publishing gh command that would expand a variable
// even when the user opts gh writes back into standard mode. The default standard
// list no longer carries these writes, so a custom config keeps explicit coverage
// of the publish guard's gh handling.
func TestIsBashCommandAllowed_VariableExpansion_GhWritesOptedIn(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tools.Bash.Mode.Standard.Allow = []string{
		`gh issue (create|edit|comment)( .*)?`,
		`gh pr create( .*)?`,
	}

	denied := []string{
		"gh issue create --title x --body $TOKEN",
		"gh issue comment 5 --body $SECRET",
		"gh pr create --title x --body $TOKEN",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed (publishes a variable's value)", cmd)
		}
	}

	allowed := []string{
		`gh issue create --body 'see $HOME'`,
		`gh pr create --title x --body 'literal $HOME'`,
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (variable is single-quoted/literal)", cmd)
		}
	}
}

// TestIsBashCommandAllowed_EnvVarAssignments verifies that SETTING env vars is
// not auto-approved - assignment prefixes (FOO=bar cmd), export, and bare
// assignments all fall through to default-deny - while USING an existing variable
// in a non-printing command stays allowed.
func TestIsBashCommandAllowed_EnvVarAssignments(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"GOOS=linux task build",
		"LANG=C sort",
		"FOO=bar ls -la",
		"export FOO=bar",
		"export FOO=$BAR",
		"FOO=bar",
		"export",
		"export -p",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed (setting env vars requires approval)", cmd)
		}
	}

	allowed := []string{
		"ls $HOME",
		"git log $REF",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (uses an existing var, no setting)", cmd)
		}
	}
}

// TestIsBashCommandAllowed_GitPushRequiresApproval locks in that no default
// pattern auto-allows "git push" in standard (or plan) mode: every push variant
// must fall through to approval. Auto mode (".*") is the deliberate exception -
// see TestIsBashCommandAllowed_AutoModeUnrestricted.
func TestIsBashCommandAllowed_GitPushRequiresApproval(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"git push",
		"git push origin main",
		"git push --force",
		"git push --force-with-lease origin feature/x",
		"git push origin feature/x",
		"git push 2>&1",
	}
	for _, mode := range []string{"standard", "plan"} {
		for _, cmd := range denied {
			if cfg.IsBashCommandAllowed(cmd, mode) {
				t.Errorf("expected %q NOT to be allowed in %s mode (push must require approval)", cmd, mode)
			}
		}
	}
}

func TestIsBashCommandAllowed_FileRedirectRestricted(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"echo hi > /tmp/out",
		"echo hi >> /tmp/out",
		"ls > out.txt",
		"git log > /etc/passwd",
		"git status > /tmp/x",
		"echo x &> out.txt",
		"echo x >& out.txt",
		"tail -n 5 /var/log/x > leak",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed (writes to a real file)", cmd)
		}
	}

	allowed := []string{
		"gh issue list >/dev/null",
		"git status 2>&1",
		"gh issue list >/dev/null 2>&1",
		`gh issue list --search "a > b"`,
		"echo 'write > file'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q to be allowed (no real file write)", cmd)
		}
	}
}

// TestIsBashCommandAllowed_FileRedirectAlwaysDenied verifies the clean-command
// guard rejects a file-write redirect BEFORE matching, so even an allow entry
// that would match the whole redirected command cannot unlock it. Auto mode
// (".*") is the deliberate exception: the sentinel skips the guard entirely.
func TestIsBashCommandAllowed_FileRedirectAlwaysDenied(t *testing.T) {
	cfg := &Config{
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Mode: BashModesConfig{
					All:  BashModeAllowConfig{Allow: []string{`echo hi > /tmp/out\.txt`}},
					Auto: BashModeAllowConfig{Allow: []string{".*"}},
				},
			},
		},
	}

	if cfg.IsBashCommandAllowed("echo hi > /tmp/out.txt", "standard") {
		t.Error("a file-write redirect must be denied even with a matching pattern")
	}
	if !cfg.IsBashCommandAllowed("echo hi > /tmp/out.txt", "auto") {
		t.Error("auto mode (.*) should allow a redirect - the guard is skipped")
	}
}

// TestIsBashCommandAllowed_PipePolicy documents that pipelines are never
// auto-approved under the single-command policy - even when both ends are
// allowed (ls | head) - which also keeps "ls | xargs rm" off the list.
func TestIsBashCommandAllowed_PipePolicy(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"ls | head",
		"echo hi | wc -l",
		"ls | xargs rm",
		"ls | tee out.txt",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected piped %q NOT to be allowed (single-command policy)", cmd)
		}
	}
}

// TestIsBashCommandAllowed_FullMatchExactness verifies entries match the WHOLE
// command: a bare token allows only itself (never a longer command), and a
// pattern must opt into arguments to accept them.
func TestIsBashCommandAllowed_FullMatchExactness(t *testing.T) {
	cfg := &Config{
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Mode: BashModesConfig{
					All: BashModeAllowConfig{Allow: []string{"gh", "git log( .*)?"}},
				},
			},
		},
	}

	if !cfg.IsBashCommandAllowed("gh", "standard") {
		t.Error("bare 'gh' should match 'gh'")
	}
	if cfg.IsBashCommandAllowed("gh issue list", "standard") {
		t.Error("bare 'gh' must not match 'gh issue list' (full-match)")
	}
	if !cfg.IsBashCommandAllowed("git log --oneline", "standard") {
		t.Error("'git log( .*)?' should match 'git log --oneline'")
	}
}

// TestIsBashCommandAllowed_ModeResolution verifies the effective allow-list for a
// mode is mode.all unioned with that mode's own list: baseline entries apply
// everywhere, the default standard list adds nothing, and a mode-specific entry
// does not leak into another mode.
func TestIsBashCommandAllowed_ModeResolution(t *testing.T) {
	cfg := DefaultConfig()

	for _, mode := range []string{"all", "plan", "standard", "auto"} {
		if !cfg.IsBashCommandAllowed("gh issue list", mode) {
			t.Errorf("baseline 'gh issue list' should be allowed in %s mode", mode)
		}
	}

	for _, mode := range []string{"plan", "standard"} {
		if cfg.IsBashCommandAllowed("gh pr create --title x", mode) {
			t.Errorf("'gh pr create' should NOT be auto-approved in %s mode (baseline only)", mode)
		}
	}

	cfg.Tools.Bash.Mode.Standard.Allow = []string{`gh pr create( .*)?`}
	if !cfg.IsBashCommandAllowed("gh pr create --title x", "standard") {
		t.Error("gh pr create should be allowed in standard once added to standard.allow")
	}
	if cfg.IsBashCommandAllowed("gh pr create --title x", "plan") {
		t.Error("gh pr create should NOT leak into plan mode (standard-only)")
	}
}

// TestIsBashCommandAllowed_AutoModeUnrestricted verifies the ".*" sentinel: in
// auto mode any single command runs without approval, including everything every
// other mode blocks (push, force-push, pipes, substitution, redirects, and a
// secret-leaking echo) because the clean-command guard is skipped.
func TestIsBashCommandAllowed_AutoModeUnrestricted(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"git push --force origin main",
		"rm -rf /tmp/x",
		"echo a | head",
		"echo $(whoami)",
		"echo $AWS_SECRET_ACCESS_KEY",
		"echo hi > /tmp/out",
		"anything --really",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandAllowed(cmd, "auto") {
			t.Errorf("expected %q to be allowed in auto mode (.*)", cmd)
		}
	}

	for _, cmd := range []string{"git push --force origin main", "echo a | head", "echo $(whoami)"} {
		if cfg.IsBashCommandAllowed(cmd, "standard") {
			t.Errorf("expected %q NOT to be allowed in standard mode", cmd)
		}
	}
}

func TestStripBenignTrailingRedirections(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"gh api repos/o/r 2>&1", "gh api repos/o/r"},
		{"gh issue list 2>/dev/null", "gh issue list"},
		{"gh issue list >/dev/null 2>&1", "gh issue list"},
		{"gh issue list 2>&1 >/dev/null", "gh issue list"},
		{"gh issue list &>/dev/null", "gh issue list"},
		{"echo hi >&2", "echo hi"},
		{"echo hi 2>&-", "echo hi"},
		{"echo hi", "echo hi"},
		{"echo /dev/null", "echo /dev/null"},
		{"gh issue list > out.txt", "gh issue list > out.txt"},
		{"2>&1", ""},
	}
	for _, tt := range tests {
		if got := stripBenignTrailingRedirections(tt.in); got != tt.want {
			t.Errorf("stripBenignTrailingRedirections(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitBashSegments(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		ok   bool
	}{
		{"gh issue list", []string{"gh issue list"}, true},
		{"a && b", []string{"a", "b"}, true},
		{"a || b", []string{"a", "b"}, true},
		{"a | b", []string{"a", "b"}, true},
		{"a |& b", []string{"a", "b"}, true},
		{"a ; b", []string{"a", "b"}, true},
		{"a & b", []string{"a", "b"}, true},
		{"gh api x 2>&1 && echo y", []string{"gh api x 2>&1", "echo y"}, true},
		{"echo hi >&2", []string{"echo hi >&2"}, true},
		{"cmd &> /dev/null", []string{"cmd &> /dev/null"}, true},
		{`echo "a && b"`, []string{`echo "a && b"`}, true},
		{"echo 'a | b'", []string{"echo 'a | b'"}, true},
		{`echo "unterminated`, nil, false},
		{"echo 'unterminated", nil, false},
	}
	for _, tt := range tests {
		got, ok := splitBashSegments(tt.in)
		if ok != tt.ok {
			t.Errorf("splitBashSegments(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		trimmed := make([]string, len(got))
		for i, s := range got {
			trimmed[i] = strings.TrimSpace(s)
		}
		if strings.Join(trimmed, "\x00") != strings.Join(tt.want, "\x00") {
			t.Errorf("splitBashSegments(%q) = %v, want %v", tt.in, trimmed, tt.want)
		}
	}
}

func TestContainsCommandSubstitution(t *testing.T) {
	yes := []string{
		"echo $(x)",
		"echo `x`",
		"cat <(x)",
		"echo >(x)",
		`echo "$(x)"`,
		"echo \"`x`\"",
	}
	for _, cmd := range yes {
		if !containsCommandSubstitution(cmd) {
			t.Errorf("containsCommandSubstitution(%q) = false, want true", cmd)
		}
	}

	no := []string{
		"echo '$(x)'",
		"echo '`x`'",
		"gh api repos/o/r --jq '.a | .b'",
		"echo $HOME",
		"echo ${HOME}",
		"gh issue create --title x --body y",
	}
	for _, cmd := range no {
		if containsCommandSubstitution(cmd) {
			t.Errorf("containsCommandSubstitution(%q) = true, want false", cmd)
		}
	}
}

func TestContainsVariableExpansion(t *testing.T) {
	yes := []string{
		"echo $HOME",
		"echo ${HOME}",
		"echo $AWS_SECRET_ACCESS_KEY",
		`echo "token=$TOKEN"`,
		`echo "${AWS_SECRET_ACCESS_KEY}"`,
		"tail $SECRETFILE",
		"echo $1",
		"echo $?",
		"echo $$",
		"echo $@",
	}
	for _, cmd := range yes {
		if !containsVariableExpansion(cmd) {
			t.Errorf("containsVariableExpansion(%q) = false, want true", cmd)
		}
	}

	no := []string{
		"echo '$HOME'",
		"echo '${HOME}'",
		`echo \$HOME`,
		"echo hi",
		"echo 5 dollars",
		"echo $(date)",
		"gh api repos/o/r --jq '.a | .b'",
		"echo a$",
		"echo price$ here",
	}
	for _, cmd := range no {
		if containsVariableExpansion(cmd) {
			t.Errorf("containsVariableExpansion(%q) = true, want false", cmd)
		}
	}
}

func TestContainsFileRedirect(t *testing.T) {
	tests := []struct {
		seg  string
		want bool
	}{
		{"echo hi > /tmp/out", true},
		{"echo hi >> /tmp/out", true},
		{"echo x &> out.txt", true},
		{"echo x >& out.txt", true},
		{"echo a>b", true},
		{"ls", false},
		{"git status", false},
		{`gh issue comment 5 --body "a > b"`, false},
		{"echo 'a > b'", false},
	}
	for _, tt := range tests {
		t.Run(tt.seg, func(t *testing.T) {
			if got := containsFileRedirect(tt.seg); got != tt.want {
				t.Errorf("containsFileRedirect(%q) = %v, want %v", tt.seg, got, tt.want)
			}
		})
	}
}

func TestBashCommandRejectionHint(t *testing.T) {
	if h := BashCommandRejectionHint("echo hi > /tmp/out"); !strings.Contains(h, "redirection") {
		t.Errorf("expected a redirection hint, got %q", h)
	}
	if h := BashCommandRejectionHint("echo $(whoami)"); !strings.Contains(h, "substitution") {
		t.Errorf("expected a command-substitution hint, got %q", h)
	}
	if h := BashCommandRejectionHint("echo $HOME"); !strings.Contains(h, "environment variable") {
		t.Errorf("expected an env-var leak hint, got %q", h)
	}
	if h := BashCommandRejectionHint("ls | head"); !strings.Contains(h, "single command") {
		t.Errorf("expected a single-command hint, got %q", h)
	}
	if h := BashCommandRejectionHint("find . -delete"); !strings.Contains(h, "find") {
		t.Errorf("expected a find-action hint, got %q", h)
	}
	if h := BashCommandRejectionHint("ls -la"); h != "" {
		t.Errorf("expected no hint for a plain command, got %q", h)
	}
	if h := BashCommandRejectionHint("ls $HOME"); h != "" {
		t.Errorf("expected no hint for using a var in a non-printing command, got %q", h)
	}
	if h := BashCommandRejectionHint("git status 2>&1"); h != "" {
		t.Errorf("expected no hint for a benign redirect, got %q", h)
	}
}
