package skills

import (
	"embed"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// builtinSkills holds the skills shipped with the binary. `infer init` seeds
// them into ~/.infer/skills, so the normal loader finds them and a project
// skill of the same name or disabled_skills still overrides them.
//
//go:embed builtins
var builtinSkills embed.FS

const builtinsRoot = "builtins"

// SeedBuiltins writes each embedded skill under destDir, the user-scope skills
// directory. A file is written only when absent unless overwrite is set, so
// user edits survive a re-run of `infer init`.
func SeedBuiltins(destDir string, overwrite bool) error {
	return utils.SeedFS(builtinSkills, builtinsRoot, destDir, overwrite)
}
