package compute

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
)

// checkAll runs check on every entry at once and returns the results in the
// entries' order. Each check writes only its own slot, so nothing is shared
// between them, and every one ends with ctx.
func checkAll[E, S any](ctx context.Context, entries []E, check func(context.Context, E) S) []S {
	results := make([]S, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		wg.Go(func() { results[i] = check(ctx, entry) })
	}
	wg.Wait()
	return results
}

// only narrows entries to the one named, or keeps them all without a name.
func only[E any](entries []E, name string, nameOf func(E) string, kind string) ([]E, error) {
	if name == "" {
		return entries, nil
	}
	index := slices.IndexFunc(entries, func(entry E) bool { return nameOf(entry) == name })
	if index < 0 {
		return nil, fmt.Errorf("%s %q is not in %s", kind, name, config.ComputeFileName)
	}
	return entries[index : index+1], nil
}

// usedBy refuses to remove compute a workspace of daemon.yaml runs on, so a
// removal never leaves a workspace pointing at nothing.
func usedBy(daemon config.DaemonConfig, compute string) error {
	if names := daemon.WorkspacesOn(compute); len(names) > 0 {
		return fmt.Errorf("%s is used by workspace %s in %s, point them elsewhere first", compute, strings.Join(names, ", "), config.DaemonFileName)
	}
	return nil
}

// editCompute applies change to the userspace compute.yaml as written, so a
// ${VAR} reference is saved back as a reference.
func editCompute(change func(*config.ComputeConfig) error) (string, error) {
	path := config.UserComputePath()
	cfg, err := config.LoadComputeFile(path)
	if err != nil {
		return "", err
	}
	if err := change(cfg); err != nil {
		return "", err
	}
	return path, config.SaveComputeFile(path, cfg)
}

func say(out io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(out, format, args...)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
