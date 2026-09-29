package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// artifactsHandler serves files the agent saved under a project's artifacts
// dir, the images an MV3 extension cannot open from a local path.
type artifactsHandler struct {
	projectsDir string
}

// newArtifactsHandler serves every project's artifacts dir under
// ~/.infer/projects, read-only.
func newArtifactsHandler() *artifactsHandler {
	return &artifactsHandler{projectsDir: filepath.Join(config.UserSpaceConfigDir(), config.ProjectsDirName)}
}

// ServeHTTP answers GET /artifacts/<project-slug>/<relative-path> with the
// file at <projectsDir>/<project-slug>/artifacts/<relative-path>. The route is
// unauthenticated like the old `infer chat` one was, but it only ever serves
// from the per-project artifacts subtree over the binding's loopback listener.
func (h *artifactsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug, rel, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/")
	if !found || slug == "" || rel == "" || slug == "." || slug != filepath.Base(slug) ||
		!filepath.IsLocal(slug) || !filepath.IsLocal(rel) {
		http.NotFound(w, r)
		return
	}

	full := filepath.Join(h.projectsDir, slug, config.ArtifactsDirName, rel)
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}
