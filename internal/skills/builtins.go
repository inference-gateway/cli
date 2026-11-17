package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// builtinSkills holds the skills shipped with the binary. `infer init` seeds
// them into ~/.infer/skills, so the normal loader finds them and a project
// skill of the same name or disabled_skills still overrides them.
//
//go:embed builtins
var builtinSkills embed.FS

const builtinsRoot = "builtins"

// SeedBuiltins writes each embedded skill under destDir, the user-scope skills
// directory, mirroring the embedded tree. A file is written only when absent
// unless overwrite is set, so user edits survive a re-run of `infer init`.
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
