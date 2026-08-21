package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// builtinSkills holds the skills shipped with the binary. They are seeded into
// the user's ~/.infer/skills on `infer init` (see SeedBuiltins) so the normal
// user-scope loader discovers them with no special-casing - a project or
// .agents skill of the same name still shadows them, and disabled_skills still
// removes them.
//
//go:embed builtins
var builtinSkills embed.FS

const builtinsRoot = "builtins"

// SeedBuiltins writes each embedded built-in skill under destDir, mirroring the
// embedded tree (destDir/<name>/SKILL.md, plus any helper files). A file is
// written only when it is absent, unless overwrite is set - so a user's edits to
// a seeded skill survive a re-run of `infer init` (seed-if-absent). destDir is
// the user-scope skills directory (~/.infer/skills), the same directory the
// loader reads user skills from.
func SeedBuiltins(destDir string, overwrite bool) error {
	return fs.WalkDir(builtinSkills, builtinsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == builtinsRoot {
			return nil
		}
		target := filepath.Join(destDir, strings.TrimPrefix(path, builtinsRoot+"/"))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !overwrite {
			if _, statErr := os.Stat(target); statErr == nil {
				return nil
			}
		}
		data, err := builtinSkills.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
