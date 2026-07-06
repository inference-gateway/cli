package plugins

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SourceKind distinguishes GitHub sources from local directories.
type SourceKind int

const (
	SourceGitHub SourceKind = iota
	SourceLocal
)

// Source is a parsed plugin install target.
type Source struct {
	Kind  SourceKind
	Owner string
	Repo  string
	Ref   string
	Path  string
	Raw   string
}

// ParseSource accepts:
//
//   - "owner/repo" and "owner/repo@ref"
//   - "https://github.com/owner/repo" and ".../tree/<ref>"
//   - local paths starting with "./", "../", "/", or "~/"
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{}, fmt.Errorf("empty plugin source")
	}

	if isLocalPath(raw) {
		return parseLocalSource(raw)
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return parseURLSource(raw)
	}
	return parseShorthandSource(raw)
}

func isLocalPath(raw string) bool {
	return strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") ||
		strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "~/") ||
		raw == "." || raw == ".."
}

func parseLocalSource(raw string) (Source, error) {
	p := raw
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Source{}, fmt.Errorf("resolving ~: %w", err)
		}
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return Source{}, fmt.Errorf("resolving local path %q: %w", raw, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Source{}, fmt.Errorf("local plugin path %s: %w", abs, err)
	}
	if !info.IsDir() {
		return Source{}, fmt.Errorf("local plugin path %s is not a directory", abs)
	}
	return Source{Kind: SourceLocal, Path: abs, Raw: abs}, nil
}

func parseURLSource(raw string) (Source, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Source{}, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Host != "github.com" {
		return Source{}, fmt.Errorf("only github.com URLs are supported, got %q", u.Host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Source{}, fmt.Errorf("URL must be of the form https://github.com/<owner>/<repo>[/tree/<ref>]: got %s", raw)
	}
	src := Source{Kind: SourceGitHub, Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	switch {
	case len(parts) == 2:
	case len(parts) >= 3 && parts[2] == "blob":
		return Source{}, fmt.Errorf("URL points to a file (/blob/) - pass the repository or /tree/<ref> URL instead: %s", raw)
	case len(parts) == 4 && parts[2] == "tree":
		src.Ref = parts[3]
	default:
		return Source{}, fmt.Errorf("URL must be of the form https://github.com/<owner>/<repo>[/tree/<ref>]: got %s", raw)
	}
	src.Raw = fmt.Sprintf("%s/%s", src.Owner, src.Repo)
	return src, nil
}

func parseShorthandSource(raw string) (Source, error) {
	spec := raw
	ref := ""
	if at := strings.LastIndex(spec, "@"); at >= 0 {
		ref = spec[at+1:]
		spec = spec[:at]
		if ref == "" {
			return Source{}, fmt.Errorf("empty ref in %q", raw)
		}
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Source{}, fmt.Errorf("plugin source must be <owner>/<repo>, a github.com URL, or a local path: got %q", raw)
	}
	return Source{Kind: SourceGitHub, Owner: parts[0], Repo: parts[1], Ref: ref, Raw: spec}, nil
}

// EffectiveRef returns the git ref to fetch: the parsed ref, or HEAD (which
// GitHub resolves to the default branch).
func (s Source) EffectiveRef() string {
	if s.Ref == "" {
		return "HEAD"
	}
	return s.Ref
}
