package vlm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// mtmdBinaryCandidates are the binary names tried, in order, when no explicit
// vision.annotator.binary_path is configured.
var mtmdBinaryCandidates = []string{"llama-mtmd-cli"}

// commandRunner runs a command and returns its stdout, abstracting exec for tests.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// LocalAnnotator annotates images by shelling out to llama.cpp's
// llama-mtmd-cli with a local GGUF vision model - no gateway, no network
// after the first model download. Mirrors stt.WhisperTranscriber.
type LocalAnnotator struct {
	cfg     *config.Config
	models  *ModelManager
	prompts config.PromptsVisionAnnotatorConfig

	// run and lookPath are overridable in tests.
	run      commandRunner
	lookPath func(string) (string, error)
}

// NewLocalAnnotator creates a local annotator from the vision config.
func NewLocalAnnotator(cfg *config.Config) *LocalAnnotator {
	return &LocalAnnotator{
		cfg:      cfg,
		models:   NewModelManager(cfg.Vision.Annotator),
		prompts:  cfg.Prompts.Vision.Annotator,
		run:      execRun,
		lookPath: exec.LookPath,
	}
}

// AnnotateImage runs llama-mtmd-cli on the image and parses the JSON reply.
func (a *LocalAnnotator) AnnotateImage(ctx context.Context, img domain.ImageAttachment, opts domain.AnnotateOptions) (*domain.ImageAnnotation, error) {
	bin, err := a.resolveBinary(ctx)
	if err != nil {
		return nil, err
	}

	llmPath, mmprojPath, err := a.models.EnsureModel(ctx)
	if err != nil {
		return nil, err
	}

	imagePath, cleanup, err := imageFile(img)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	timeout := a.cfg.Vision.Annotator.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	maxTokens := a.cfg.Vision.Annotator.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	out, err := a.run(ctx, bin,
		"-m", llmPath,
		"--mmproj", mmprojPath,
		"--image", imagePath,
		"-p", a.buildPrompt(opts),
		"-n", strconv.Itoa(maxTokens),
		"--temp", "0",
	)
	if err != nil {
		return nil, fmt.Errorf("image annotation failed: %w", err)
	}

	annotation := parseAnnotation(string(out))
	rescaleBBoxes(annotation, opts.Width, opts.Height)
	return annotation, nil
}

// buildPrompt combines the task prompt with the strict-JSON contract. The
// local engine asks for 0-1000-normalized bboxes (Qwen-VL's native grounding
// convention) and rescales afterwards, sidestepping resolution mismatches.
func (a *LocalAnnotator) buildPrompt(opts domain.AnnotateOptions) string {
	task := strings.TrimSpace(opts.Prompt)
	if task == "" {
		task = a.prompts.SceneSystemPrompt
	}
	prompt := task
	if opts.Width > 0 && opts.Height > 0 {
		prompt += fmt.Sprintf("\nThe image is %dx%d pixels.", opts.Width, opts.Height)
	}
	return prompt + "\n" + fmt.Sprintf(jsonContract, "bbox coordinates must be normalized to the range 0-1000.")
}

// resolveBinary returns the llama-mtmd-cli binary to invoke: an explicit
// configured path first, then PATH lookup. Auto-download of a prebuilt static
// binary (like stt-binaries) is a follow-up once release assets exist.
func (a *LocalAnnotator) resolveBinary(_ context.Context) (string, error) {
	if p := strings.TrimSpace(a.cfg.Vision.Annotator.BinaryPath); p != "" {
		if _, err := a.lookPath(p); err == nil {
			return p, nil
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("configured vision.annotator.binary_path %q not found or not executable", p)
	}

	for _, name := range mtmdBinaryCandidates {
		if _, err := a.lookPath(name); err == nil {
			return name, nil
		}
	}

	return "", fmt.Errorf("llama-mtmd-cli not found: install llama.cpp "+
		"(e.g. `brew install llama.cpp`, nix `llama-cpp`, or build from "+
		"https://github.com/ggml-org/llama.cpp) or set vision.annotator.binary_path, "+
		"or use vision.annotator.engine \"gateway\" with a provider/model (tried %s)",
		strings.Join(mtmdBinaryCandidates, ", "))
}

// imageFile returns a readable file path for the attachment: its SourcePath
// when it exists, otherwise the base64 data written to a temp file.
func imageFile(img domain.ImageAttachment) (string, func(), error) {
	if p := strings.TrimSpace(img.SourcePath); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, func() {}, nil
		}
	}

	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "", nil, fmt.Errorf("decoding image data: %w", err)
	}
	ext := "png"
	if i := strings.LastIndex(img.MimeType, "/"); i >= 0 && i < len(img.MimeType)-1 {
		ext = img.MimeType[i+1:]
	}
	tmp, err := os.CreateTemp("", "infer-annotate-*."+ext)
	if err != nil {
		return "", nil, fmt.Errorf("creating temp image file: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("writing temp image file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("closing temp image file: %w", err)
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

// execRun runs name with args and returns stdout, wrapping failures with stderr.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
