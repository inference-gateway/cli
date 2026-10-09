//go:build linux

package accessibility

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	stateBitEnabled = uint64(1) << stateEnabled
	stateBitFocused = uint64(1) << stateFocused
)

type fakeNode struct {
	role     string
	name     string
	states   uint64
	box      rect
	actions  []string
	children []accessible
}

type fakeBus struct {
	apps        []accessible
	pids        map[string]uint32
	nodes       map[accessible]fakeNode
	refuse      bool
	pressed     []string
	unsupported []string
}

func (f *fakeBus) node(node accessible) (fakeNode, error) {
	if n, ok := f.nodes[node]; ok {
		return n, nil
	}
	return fakeNode{}, fmt.Errorf("unknown object %s %s", node.Bus, node.Path)
}

func (f *fakeBus) applications(context.Context) ([]accessible, error) {
	return f.apps, nil
}

func (f *fakeBus) processID(_ context.Context, bus string) (uint32, error) {
	if pid, ok := f.pids[bus]; ok {
		return pid, nil
	}
	return 0, fmt.Errorf("unknown connection %s", bus)
}

func (f *fakeBus) children(_ context.Context, node accessible) ([]accessible, error) {
	n, err := f.node(node)
	return n.children, err
}

func (f *fakeBus) roleName(_ context.Context, node accessible) (string, error) {
	n, err := f.node(node)
	return n.role, err
}

func (f *fakeBus) interfaces(_ context.Context, node accessible) ([]string, error) {
	n, err := f.node(node)
	interfaces := []string{"org.a11y.atspi.Accessible"}
	if n.box != (rect{}) {
		interfaces = append(interfaces, componentInterface)
	}
	if n.actions != nil {
		interfaces = append(interfaces, actionInterface)
	}
	return interfaces, err
}

func (f *fakeBus) name(_ context.Context, node accessible) (string, error) {
	n, err := f.node(node)
	return n.name, err
}

func (f *fakeBus) states(_ context.Context, node accessible) (uint64, error) {
	n, err := f.node(node)
	return n.states, err
}

func (f *fakeBus) extents(_ context.Context, node accessible) (rect, error) {
	n, err := f.node(node)
	if err == nil && n.box == (rect{}) {
		f.unsupported = append(f.unsupported, "GetExtents "+node.Path)
		err = errors.New("no Component interface")
	}
	return n.box, err
}

func (f *fakeBus) actionNames(_ context.Context, node accessible) ([]string, error) {
	n, err := f.node(node)
	if err == nil && n.actions == nil {
		f.unsupported = append(f.unsupported, "NActions "+node.Path)
	}
	return n.actions, err
}

func (f *fakeBus) doAction(_ context.Context, node accessible, index int) (bool, error) {
	f.pressed = append(f.pressed, fmt.Sprintf("%s#%d", node.Path, index))
	return !f.refuse, nil
}

func ref(bus, path string) accessible {
	return accessible{Bus: bus, Path: path}
}

func TestCollectFollowsReferencesAcrossBusNames(t *testing.T) {
	app := ref(":1.10", "/root")
	frame := ref(":1.10", "/frame")
	socket := ref(":1.10", "/socket")
	webRoot := ref(":1.20", "/root")
	send := ref(":1.20", "/send")
	offscreen := ref(":1.20", "/offscreen")
	bus := &fakeBus{nodes: map[accessible]fakeNode{
		app:       {role: "application", name: "desktop", children: []accessible{frame}},
		frame:     {role: "frame", name: "Desktop", states: stateBitEnabled, box: rect{0, 0, 800, 600}, children: []accessible{socket, ref("", nullPath)}},
		socket:    {role: "filler", box: rect{0, 40, 800, 560}, children: []accessible{webRoot}},
		webRoot:   {role: "document web", box: rect{0, 40, 800, 560}, actions: []string{""}, children: []accessible{send, offscreen}},
		send:      {role: "push button", name: " Send ", states: stateBitEnabled | stateBitFocused, box: rect{10, 20, 50, 20}, actions: []string{"press"}},
		offscreen: {role: "push button", name: "Hidden", actions: []string{"press"}},
	}}

	got := collect(t.Context(), bus, app)
	want := []computerdomain.UIElement{
		{Role: "frame", Label: "Desktop", State: "enabled", BBox: [4]int{0, 0, 800, 600}},
		{Role: "push button", Label: "Send", State: "enabled focused actions=press", BBox: [4]int{10, 20, 60, 40}},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("collect() = %+v, want %+v", got, want)
	}
	if len(bus.unsupported) > 0 {
		t.Fatalf("collect() called interfaces the objects lack: %q", bus.unsupported)
	}
}

func TestCollectStopsAtTreeLimits(t *testing.T) {
	tests := []struct {
		name  string
		nodes map[accessible]fakeNode
		want  int
	}{
		{name: "cycle", nodes: cycleTree(), want: 2},
		{name: "depth", nodes: chainTree(maxTreeDepth + 8), want: maxTreeDepth},
		{name: "element cap", nodes: wideTree(maxTreeElements + 50), want: maxTreeElements},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collect(t.Context(), &fakeBus{nodes: tt.nodes}, ref(":1.1", "/0"))
			if len(got) != tt.want {
				t.Fatalf("collect() returned %d elements, want %d", len(got), tt.want)
			}
		})
	}
}

func button(name string, children ...accessible) fakeNode {
	return fakeNode{role: "push button", name: name, box: rect{1, 1, 10, 10}, actions: []string{"click"}, children: children}
}

func cycleTree() map[accessible]fakeNode {
	return map[accessible]fakeNode{
		ref(":1.1", "/0"): {role: "application", children: []accessible{ref(":1.1", "/1")}},
		ref(":1.1", "/1"): button("One", ref(":1.1", "/2")),
		ref(":1.1", "/2"): button("Two", ref(":1.1", "/1"), ref(":1.1", "/0")),
	}
}

func chainTree(depth int) map[accessible]fakeNode {
	nodes := map[accessible]fakeNode{ref(":1.1", "/0"): {role: "application", children: []accessible{ref(":1.1", "/1")}}}
	for i := 1; i <= depth; i++ {
		nodes[ref(":1.1", fmt.Sprintf("/%d", i))] = button(fmt.Sprint(i), ref(":1.1", fmt.Sprintf("/%d", i+1)))
	}
	return nodes
}

func wideTree(width int) map[accessible]fakeNode {
	root := fakeNode{role: "application"}
	nodes := map[accessible]fakeNode{}
	for i := 1; i <= width; i++ {
		child := ref(":1.1", fmt.Sprintf("/%d", i))
		root.children = append(root.children, child)
		nodes[child] = button(fmt.Sprint(i))
	}
	nodes[ref(":1.1", "/0")] = root
	return nodes
}

func TestPressActionIndexAndReportedActions(t *testing.T) {
	tests := []struct {
		actions   []string
		wantIndex int
		wantNames []string
	}{
		{actions: []string{"press"}, wantIndex: 0, wantNames: []string{"press"}},
		{actions: []string{"Click"}, wantIndex: 0, wantNames: []string{"press"}},
		{actions: []string{"release", "click", "press"}, wantIndex: 2, wantNames: []string{"release", "click", "press"}},
		{actions: []string{"jump"}, wantIndex: 0, wantNames: []string{"press"}},
		{actions: []string{"activate", ""}, wantIndex: 0, wantNames: []string{"press"}},
		{actions: []string{"", "jump"}, wantIndex: 1, wantNames: []string{"press"}},
		{actions: []string{""}, wantIndex: -1, wantNames: []string{}},
		{actions: []string{"Expand", "collapse"}, wantIndex: -1, wantNames: []string{"expand", "collapse"}},
		{actions: nil, wantIndex: -1, wantNames: []string{}},
	}
	for _, tt := range tests {
		if got := pressActionIndex(tt.actions); got != tt.wantIndex {
			t.Errorf("pressActionIndex(%q) = %d, want %d", tt.actions, got, tt.wantIndex)
		}
		if got := reportedActions(tt.actions); !slices.Equal(got, tt.wantNames) {
			t.Errorf("reportedActions(%q) = %q, want %q", tt.actions, got, tt.wantNames)
		}
	}
}

func TestPress(t *testing.T) {
	app := ref(":1.1", "/0")
	nodes := map[accessible]fakeNode{
		app:                   {role: "application", children: []accessible{ref(":1.1", "/label"), ref(":1.1", "/send"), ref(":1.2", "/link"), ref(":1.1", "/menu")}},
		ref(":1.1", "/label"): {role: "label", name: "Send"},
		ref(":1.1", "/send"):  {role: "push button", name: "Send", actions: []string{"Click", "press"}},
		ref(":1.2", "/link"):  {role: "link", name: "Docs", actions: []string{"jump"}},
		ref(":1.1", "/menu"):  {role: "menu", name: "Menu", actions: []string{"expand", "collapse"}},
	}
	tests := []struct {
		name        string
		label       string
		refuse      bool
		wantErr     error
		wantPressed []string
	}{
		{name: "prefers press over click", label: "Send", wantPressed: []string{"/send#1"}},
		{name: "sole action", label: "Docs", wantPressed: []string{"/link#0"}},
		{name: "case sensitive", label: "send", wantErr: ErrElementNotFound},
		{name: "no press action", label: "Menu", wantErr: ErrElementNotFound},
		{name: "empty label", label: "", wantErr: ErrElementNotFound},
		{name: "DoAction refused", label: "Send", refuse: true, wantErr: ErrUnavailable, wantPressed: []string{"/send#1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := &fakeBus{nodes: nodes, refuse: tt.refuse}
			err := press(t.Context(), bus, app, tt.label)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("press(%q) error = %v, want %v", tt.label, err, tt.wantErr)
			}
			if !slices.Equal(bus.pressed, tt.wantPressed) {
				t.Fatalf("press(%q) pressed %q, want %q", tt.label, bus.pressed, tt.wantPressed)
			}
			if len(bus.unsupported) > 0 {
				t.Fatalf("press(%q) called interfaces the objects lack: %q", tt.label, bus.unsupported)
			}
		})
	}
}

func TestRun(t *testing.T) {
	app := ref(":1.6", "/root")
	bus := &fakeBus{
		apps: []accessible{ref(":1.5", "/root"), app},
		pids: map[string]uint32{":1.5": 10, ":1.6": 20},
		nodes: map[accessible]fakeNode{
			app:                  {role: "application"},
			ref(":1.5", "/root"): {role: "application"},
		},
	}
	tests := []struct {
		name    string
		pid     uint32
		action  string
		want    []computerdomain.UIElement
		wantErr error
	}{
		{name: "no application for pid", pid: 30, action: "elements", wantErr: ErrUnavailable},
		{name: "unknown action", pid: 20, action: "zoom", wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := run(t.Context(), bus, tt.pid, request{Action: tt.action})
			if !errors.Is(err, tt.wantErr) || !slices.Equal(got, tt.want) {
				t.Fatalf("run() = %+v, %v, want %+v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestRunNativeFailsFastWithoutAccessibility(t *testing.T) {
	tests := []struct {
		name    string
		wayland string
		target  string
		wantErr error
	}{
		{name: "wayland session", wayland: "wayland-0", target: "pid:1", wantErr: ErrUnsupported},
		{name: "macOS-only target", target: "dock", wantErr: ErrUnsupported},
		{name: "invalid pid", target: "pid:abc", wantErr: ErrUnavailable},
		{name: "no accessibility bus", target: "pid:1", wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", tt.wayland)
			t.Setenv("DISPLAY", ":0")
			t.Setenv("AT_SPI_BUS_ADDRESS", "unix:path=/nonexistent/at-spi-bus")
			start := time.Now()
			_, err := runNative(request{Action: "elements", Target: tt.target})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("runNative() error = %v, want %v", err, tt.wantErr)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("runNative() took %v, want a fast failure", elapsed)
			}
		})
	}
}

const gtkButtonApp = `
import sys
import gi
gi.require_version("Gtk", "3.0")
from gi.repository import GLib
GLib.set_prgname("infer-atspi-test")
from gi.repository import Gtk
pressed = []
window = Gtk.Window(title="infer AT-SPI test")
button = Gtk.Button(label="Press me")
button.connect("clicked", lambda *_: (pressed.append(True), Gtk.main_quit()))
window.connect("destroy", Gtk.main_quit)
window.add(button)
window.show_all()
GLib.timeout_add_seconds(30, Gtk.main_quit)
Gtk.main()
sys.exit(0 if pressed else 1)
`

func TestAtspiIntegrationPressesGtkButton(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("needs an X11 display and a session bus: xvfb-run dbus-run-session go test")
	}
	if err := exec.Command("python3", "-c", `import gi; gi.require_version("Gtk", "3.0")`).Run(); err != nil {
		t.Skip("needs python3-gi with GTK 3")
	}
	var output bytes.Buffer
	app := exec.CommandContext(t.Context(), "python3", "-c", gtkButtonApp)
	app.Stdout, app.Stderr = &output, &output
	if err := app.Start(); err != nil {
		t.Fatalf("start GTK app: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- app.Wait() }()

	button := waitForElement(t, "app:infer-atspi-test", "Press me")
	if button.Role != "push button" || button.BBox[2] <= button.BBox[0] || button.BBox[3] <= button.BBox[1] {
		t.Fatalf("button = %+v, want a push button with a screen bbox", button)
	}
	if _, err := runNative(request{Action: "press", Target: "frontmost", Label: "Press me"}); err != nil {
		t.Fatalf("press: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("GTK app exited with %v, want 0 after the press. Output:\n%s", err, output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GTK app is still running 10s after the press")
	}
}

func waitForElement(t *testing.T, target, label string) computerdomain.UIElement {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		elements, err := runNative(request{Action: "elements", Target: target})
		if i := slices.IndexFunc(elements, func(e computerdomain.UIElement) bool { return e.Label == label }); i >= 0 {
			return elements[i]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q element in %s: elements = %+v, err = %v", label, target, elements, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
