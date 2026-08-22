// Package accessibility provides crash-isolated platform accessibility-tree
// access for the computer-use capability.
package accessibility

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	helperEnvironment = "INFER_INTERNAL_ACCESSIBILITY_HELPER"
	helperTimeout     = 8 * time.Second
)

var (
	ErrUnsupported     = errors.New("accessibility tree is unsupported on this platform")
	ErrPermission      = errors.New("accessibility permission is not granted")
	ErrUnavailable     = errors.New("accessibility tree is unavailable")
	ErrElementNotFound = errors.New("accessibility element was not found")
)

// Provider reads and acts on the host accessibility tree.
type Provider interface {
	Elements(ctx context.Context, target string) ([]computerdomain.UIElement, error)
	Press(ctx context.Context, target, label string) error
}

type request struct {
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Label  string `json:"label,omitempty"`
}

type response struct {
	Elements []computerdomain.UIElement `json:"elements,omitempty"`
	Code     string                     `json:"code,omitempty"`
	Error    string                     `json:"error,omitempty"`
}

type commandFactory func(context.Context) (*exec.Cmd, error)

type subprocessProvider struct {
	command commandFactory
	timeout time.Duration
}

type unsupportedProvider struct{}

// NewProvider returns the platform provider. Native API calls are made only
// by a short-lived helper process, so a fatal foreign-library fault cannot
// terminate the CLI process.
func NewProvider() Provider {
	if runtime.GOOS != "darwin" {
		return unsupportedProvider{}
	}
	return &subprocessProvider{command: helperCommand, timeout: helperTimeout}
}

func (unsupportedProvider) Elements(context.Context, string) ([]computerdomain.UIElement, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}

func (unsupportedProvider) Press(context.Context, string, string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, runtime.GOOS)
}

func (p *subprocessProvider) Elements(ctx context.Context, target string) ([]computerdomain.UIElement, error) {
	resp, err := p.call(ctx, request{Action: "elements", Target: target})
	if err != nil {
		return nil, err
	}
	return resp.Elements, nil
}

func (p *subprocessProvider) Press(ctx context.Context, target, label string) error {
	_, err := p.call(ctx, request{Action: "press", Target: target, Label: label})
	return err
}

func (p *subprocessProvider) call(ctx context.Context, req request) (response, error) {
	timeout := p.timeout
	if timeout <= 0 {
		timeout = helperTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := json.Marshal(req)
	if err != nil {
		return response{}, fmt.Errorf("encode accessibility helper request: %w", err)
	}
	cmd, err := p.command(ctx)
	if err != nil {
		return response{}, fmt.Errorf("start accessibility helper: %w", err)
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return response{}, fmt.Errorf("%w: helper timed out: %v", ErrUnavailable, ctx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return response{}, fmt.Errorf("%w: helper exited: %v: %s", ErrUnavailable, err, detail)
		}
		return response{}, fmt.Errorf("%w: helper exited: %v", ErrUnavailable, err)
	}

	var resp response
	if err := json.NewDecoder(&stdout).Decode(&resp); err != nil {
		return response{}, fmt.Errorf("%w: decode helper response: %v", ErrUnavailable, err)
	}
	if resp.Error != "" {
		return response{}, responseError(resp)
	}
	return resp, nil
}

func helperCommand(ctx context.Context) (*exec.Cmd, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable)
	cmd.Env = append(os.Environ(), helperEnvironment+"=1")
	return cmd, nil
}

func responseError(resp response) error {
	var kind error
	switch resp.Code {
	case "unsupported":
		kind = ErrUnsupported
	case "permission":
		kind = ErrPermission
	case "not_found":
		kind = ErrElementNotFound
	default:
		kind = ErrUnavailable
	}
	return fmt.Errorf("%w: %s", kind, resp.Error)
}
