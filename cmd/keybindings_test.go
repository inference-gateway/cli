package cmd

import (
	"slices"
	"testing"
)

// TestGetValidActionIDs_IncludesNamespacePathActions ensures the CLI validator's
// notion of "valid" matches the runtime: registry-backed actions unioned with the
// namespace-path actions (diff_viewer_*, explorer_*, chat_focus_attachments) that
// components resolve via config.ResolveNamespaceBindings. Before the fix these were
// wrongly reported as unknown by `infer keybindings validate`.
func TestGetValidActionIDs_IncludesNamespacePathActions(t *testing.T) {
	ids := getValidActionIDs()

	want := []string{
		"global_quit",
		"chat_tab_key_handler",
		"chat_focus_attachments",
		"diff_viewer_nav_up",
		"explorer_find",
	}
	for _, id := range want {
		if !slices.Contains(ids, id) {
			t.Errorf("getValidActionIDs() missing %q", id)
		}
	}
}
