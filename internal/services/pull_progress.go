package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// dockerLayerRe matches docker's non-TTY per-layer output: "<layer-id>: <status>".
var dockerLayerRe = regexp.MustCompile(`^([0-9a-f]{10,64}): (.+)$`)

// podmanBlobRe matches podman's "Copying blob <id> ..." output lines.
var podmanBlobRe = regexp.MustCompile(`^Copying blob (\S+)`)

// pullProgress counts image layers from docker/podman non-TTY pull output.
// ponytail: heuristic layer counting; docker gives real totals, podman only
// emits done-lines so the bar jumps — good enough to show liveness.
type pullProgress struct {
	layers    map[string]bool // layer id -> completed
	lastDone  int
	lastTotal int
}

func newPullProgress() *pullProgress {
	return &pullProgress{layers: make(map[string]bool), lastDone: -1, lastTotal: -1}
}

// parse consumes one output line and returns the current layer counts plus
// whether they changed since the last call.
func (p *pullProgress) parse(line string) (done, total int, changed bool) {
	if m := dockerLayerRe.FindStringSubmatch(line); m != nil {
		p.recordDockerLine(m[1], m[2])
	} else if m := podmanBlobRe.FindStringSubmatch(line); m != nil {
		p.recordLayer(m[1], strings.HasSuffix(line, " done"))
	}

	for _, completed := range p.layers {
		if completed {
			done++
		}
	}
	total = len(p.layers)
	changed = done != p.lastDone || total != p.lastTotal
	p.lastDone, p.lastTotal = done, total
	return done, total, changed
}

func (p *pullProgress) recordDockerLine(id, status string) {
	if strings.HasPrefix(status, "Pulling from") {
		return
	}
	p.recordLayer(id, status == "Pull complete" || status == "Already exists")
}

func (p *pullProgress) recordLayer(id string, completed bool) {
	if completed {
		p.layers[id] = true
	} else if _, seen := p.layers[id]; !seen {
		p.layers[id] = false
	}
}

// runPullCommand streams `<binary> pull <image>` output, reporting layer
// progress via the optional progress callback.
func runPullCommand(ctx context.Context, binary, image string, progress func(done, total int)) error {
	cmd := exec.CommandContext(ctx, binary, "pull", image)

	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	pp := newPullProgress()
	var lastLine string
	var wg sync.WaitGroup
	wg.Go(func() {
		scanner := bufio.NewScanner(pipeReader)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lastLine = line
			if done, total, changed := pp.parse(line); changed && progress != nil {
				progress(done, total)
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			logger.Debug("pull output scan ended with error", "binary", binary, "error", scanErr)
		}
	})

	err := cmd.Run()
	_ = pipeWriter.Close()
	wg.Wait()

	if err != nil {
		return fmt.Errorf("%s pull failed: %w, output: %s", binary, err, lastLine)
	}
	return nil
}
