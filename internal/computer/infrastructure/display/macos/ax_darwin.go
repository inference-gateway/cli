//go:build darwin

package macos

// macOS accessibility backend, cgo-free. It drives the AXUIElement C API
// (ApplicationServices) and NSWorkspace (AppKit, via the ObjC runtime) through
// purego, so the package builds with CGO_ENABLED=0 and cross-compiles.
//
// Frameworks are loaded lazily on first use (axEnsureLoaded); the ObjC calls
// run inside an autorelease pool and every CoreFoundation value copied out of
// the tree is released, so a long-running agent does not leak.

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	kCFStringEncodingUTF8 = 0x08000100
	kAXValueCGPointType   = 1
	kAXValueCGSizeType    = 2
	axErrorSuccess        = 0
	axMaxDepth            = 8
	// axMaxElements caps a list walk.
	// ponytail: fixed cap; make configurable if real apps overflow.
	axMaxElements = 200
)

type cgPoint struct{ X, Y float64 }
type cgSize struct{ W, H float64 }

// CoreFoundation
var (
	cfRelease                 func(uintptr)
	cfGetTypeID               func(uintptr) uint
	cfStringGetTypeID         func() uint
	cfArrayGetTypeID          func() uint
	cfStringCreateWithCString func(alloc uintptr, cstr *byte, enc uint32) uintptr
	cfStringGetLength         func(uintptr) int
	cfStringGetCString        func(s uintptr, buf *byte, bufSize int, enc uint32) bool
	cfArrayGetCount           func(uintptr) int
	cfArrayGetValueAtIndex    func(uintptr, int) uintptr
)

// ApplicationServices / HIServices (AXUIElement)
var (
	axIsProcessTrusted   func() bool
	axCreateApplication  func(pid int32) uintptr
	axCopyAttributeValue func(el, attr uintptr, out *uintptr) int32
	axCopyActionNames    func(el uintptr, out *uintptr) int32
	axPerformAction      func(el, action uintptr) int32
	axValueGetValue      func(value uintptr, theType uint32, valuePtr unsafe.Pointer) bool
)

// libobjc autorelease pool
var (
	objcAutoreleasePoolPush func() uintptr
	objcAutoreleasePoolPop  func(uintptr)
)

var (
	axLoadOnce sync.Once
	axLoadErr  error

	// attribute / action names, created once from Go literals (no need to
	// resolve the framework's kAX* CFString data symbols).
	sRole, sTitle, sDesc, sPos, sSize, sChildren, sMenuBar, sPress uintptr
)

func axDlopen(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(fmt.Sprintf("dlopen %s: %v", path, err))
	}
	return h
}

func axSetup() {
	defer func() {
		if r := recover(); r != nil {
			axLoadErr = fmt.Errorf("macOS accessibility init failed: %v", r)
		}
	}()

	// Loading Foundation + AppKit registers their ObjC classes (NSWorkspace,
	// NSRunningApplication, NSString, NSArray) so objc.GetClass resolves them.
	axDlopen("/System/Library/Frameworks/Foundation.framework/Foundation")
	axDlopen("/System/Library/Frameworks/AppKit.framework/AppKit")
	cf := axDlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	as := axDlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
	oc := axDlopen("/usr/lib/libobjc.A.dylib")

	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
	purego.RegisterLibFunc(&cfStringGetTypeID, cf, "CFStringGetTypeID")
	purego.RegisterLibFunc(&cfArrayGetTypeID, cf, "CFArrayGetTypeID")
	purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfStringGetLength, cf, "CFStringGetLength")
	purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")
	purego.RegisterLibFunc(&cfArrayGetCount, cf, "CFArrayGetCount")
	purego.RegisterLibFunc(&cfArrayGetValueAtIndex, cf, "CFArrayGetValueAtIndex")

	purego.RegisterLibFunc(&axIsProcessTrusted, as, "AXIsProcessTrusted")
	purego.RegisterLibFunc(&axCreateApplication, as, "AXUIElementCreateApplication")
	purego.RegisterLibFunc(&axCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
	purego.RegisterLibFunc(&axCopyActionNames, as, "AXUIElementCopyActionNames")
	purego.RegisterLibFunc(&axPerformAction, as, "AXUIElementPerformAction")
	purego.RegisterLibFunc(&axValueGetValue, as, "AXValueGetValue")

	purego.RegisterLibFunc(&objcAutoreleasePoolPush, oc, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&objcAutoreleasePoolPop, oc, "objc_autoreleasePoolPop")

	sRole = cfStr("AXRole")
	sTitle = cfStr("AXTitle")
	sDesc = cfStr("AXDescription")
	sPos = cfStr("AXPosition")
	sSize = cfStr("AXSize")
	sChildren = cfStr("AXChildren")
	sMenuBar = cfStr("AXMenuBar")
	sPress = cfStr("AXPress")
}

func axEnsureLoaded() error {
	axLoadOnce.Do(axSetup)
	return axLoadErr
}

// axProcessTrusted reports whether this process holds the macOS Accessibility
// (TCC) permission. Shared with the display Provider readiness check.
func axProcessTrusted() bool {
	if axEnsureLoaded() != nil {
		return false
	}
	return axIsProcessTrusted()
}

// cfStr creates a CFString from a Go literal. The result is retained for the
// process lifetime (attribute names), so it is never released.
func cfStr(s string) uintptr {
	b := append([]byte(s), 0)
	r := cfStringCreateWithCString(0, &b[0], kCFStringEncodingUTF8)
	return r
}

func cfStringToGo(s uintptr) string {
	if s == 0 {
		return ""
	}
	n := cfStringGetLength(s)
	if n == 0 {
		return ""
	}
	buf := make([]byte, n*4+1)
	if cfStringGetCString(s, &buf[0], len(buf), kCFStringEncodingUTF8) {
		if i := bytes.IndexByte(buf, 0); i >= 0 {
			return string(buf[:i])
		}
		return string(buf)
	}
	return ""
}

// attrString reads a string attribute, releasing the copied value.
func attrString(el, attr uintptr) string {
	var v uintptr
	if axCopyAttributeValue(el, attr, &v) != axErrorSuccess || v == 0 {
		return ""
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfStringGetTypeID() {
		return ""
	}
	return cfStringToGo(v)
}

// elementTitle: AXTitle, falling back to AXDescription (toolbar buttons and
// dock items often only set the latter).
func elementTitle(el uintptr) string {
	if t := attrString(el, sTitle); t != "" {
		return t
	}
	return attrString(el, sDesc)
}

func isPressable(el uintptr) bool {
	var actions uintptr
	if axCopyActionNames(el, &actions) != axErrorSuccess || actions == 0 {
		return false
	}
	defer cfRelease(actions)
	n := cfArrayGetCount(actions)
	for i := range n {
		if cfStringToGo(cfArrayGetValueAtIndex(actions, i)) == "AXPress" {
			return true
		}
	}
	return false
}

func elementFrame(el uintptr) (cgPoint, cgSize, bool) {
	var pv uintptr
	if axCopyAttributeValue(el, sPos, &pv) != axErrorSuccess || pv == 0 {
		return cgPoint{}, cgSize{}, false
	}
	defer cfRelease(pv)
	var pt cgPoint
	if !axValueGetValue(pv, kAXValueCGPointType, unsafe.Pointer(&pt)) {
		return cgPoint{}, cgSize{}, false
	}
	var sv uintptr
	if axCopyAttributeValue(el, sSize, &sv) != axErrorSuccess || sv == 0 {
		return cgPoint{}, cgSize{}, false
	}
	defer cfRelease(sv)
	var sz cgSize
	if !axValueGetValue(sv, kAXValueCGSizeType, unsafe.Pointer(&sz)) {
		return cgPoint{}, cgSize{}, false
	}
	return pt, sz, true
}

// forEachChild calls fn for each child of el, releasing the children array.
func forEachChild(el uintptr, fn func(child uintptr) bool) {
	var children uintptr
	if axCopyAttributeValue(el, sChildren, &children) != axErrorSuccess || children == 0 {
		return
	}
	defer cfRelease(children)
	if cfGetTypeID(children) != cfArrayGetTypeID() {
		return
	}
	n := cfArrayGetCount(children)
	for i := range n {
		if !fn(cfArrayGetValueAtIndex(children, i)) {
			return
		}
	}
}

// appendPressable appends el as an annotated element when it is a pressable,
// titled, positioned control; otherwise it is a no-op.
func appendPressable(el uintptr, out *[]agentdomain.AnnotatedElement) {
	if !isPressable(el) {
		return
	}
	title := elementTitle(el)
	if title == "" {
		return
	}
	pt, sz, ok := elementFrame(el)
	if !ok {
		return
	}
	role := attrString(el, sRole)
	if role == "" {
		role = "AXUnknown"
	}
	*out = append(*out, agentdomain.AnnotatedElement{
		Index: len(*out) + 1,
		Label: normalizeAXRole(role),
		Text:  title,
		BBox:  [4]int{int(pt.X), int(pt.Y), int(pt.X + sz.W), int(pt.Y + sz.H)},
	})
}

func walkCollect(el uintptr, depth, maxDepth int, out *[]agentdomain.AnnotatedElement) {
	if el == 0 || depth > maxDepth || len(*out) >= axMaxElements {
		return
	}
	appendPressable(el, out)
	forEachChild(el, func(child uintptr) bool {
		walkCollect(child, depth+1, maxDepth, out)
		return len(*out) < axMaxElements
	})
}

// pressFirst performs kAXPress on the first pressable element titled label,
// while it is still owned by its live parent array.
func pressFirst(el uintptr, label string, depth, maxDepth int) (found bool, axErr int32) {
	if el == 0 || depth > maxDepth {
		return false, 0
	}
	if isPressable(el) && elementTitle(el) == label {
		return true, axPerformAction(el, sPress)
	}
	forEachChild(el, func(child uintptr) bool {
		if f, e := pressFirst(child, label, depth+1, maxDepth); f {
			found, axErr = true, e
			return false
		}
		return true
	})
	return found, axErr
}

func sel(s string) objc.SEL { return objc.RegisterName(s) }

// nsString reads an NSString via the toll-free NSString/CFString bridge.
func nsString(id objc.ID) string {
	if id == 0 {
		return ""
	}
	return cfStringToGo(uintptr(id))
}

func sharedWorkspace() objc.ID {
	return objc.ID(objc.GetClass("NSWorkspace")).Send(sel("sharedWorkspace"))
}

func frontmostPID() int32 {
	app := sharedWorkspace().Send(sel("frontmostApplication"))
	if app == 0 {
		return 0
	}
	return int32(app.Send(sel("processIdentifier")))
}

func dockPID() int32 {
	arr := sharedWorkspace().Send(sel("runningApplications"))
	if arr == 0 {
		return 0
	}
	n := int(arr.Send(sel("count")))
	for i := range n {
		app := arr.Send(sel("objectAtIndex:"), uint(i))
		if nsString(app.Send(sel("bundleIdentifier"))) == "com.apple.dock" {
			return int32(app.Send(sel("processIdentifier")))
		}
	}
	return 0
}

// axRoot resolves the tree root for a target and the walk depth. The returned
// element must be released with cfRelease. ok is false when it cannot resolve.
func axRoot(target string) (root uintptr, maxDepth int, ok bool) {
	switch target {
	case "dock":
		pid := dockPID()
		if pid == 0 {
			return 0, 0, false
		}
		return axCreateApplication(pid), axMaxDepth, true
	case "menubar":
		pid := frontmostPID()
		if pid == 0 {
			return 0, 0, false
		}
		app := axCreateApplication(pid)
		var mb uintptr
		rc := axCopyAttributeValue(app, sMenuBar, &mb)
		cfRelease(app)
		if rc != axErrorSuccess || mb == 0 {
			return 0, 0, false
		}
		// Only the top-level menu titles are meaningful while menus are closed.
		return mb, 1, true
	default: // "" or "frontmost"
		pid := frontmostPID()
		if pid == 0 {
			return 0, 0, false
		}
		return axCreateApplication(pid), axMaxDepth, true
	}
}

// axProvider implements display.AXProvider via the AXUIElement C API driven
// through purego. Requires the Accessibility (TCC) permission; without it every
// call returns display.ErrNoAXPermission.
type axProvider struct{}

var _ display.AXProvider = (*axProvider)(nil)

func (axProvider) ListElements(_ context.Context, target string) ([]agentdomain.AnnotatedElement, error) {
	if err := axEnsureLoaded(); err != nil {
		return nil, err
	}
	pool := objcAutoreleasePoolPush()
	defer objcAutoreleasePoolPop(pool)

	if !axIsProcessTrusted() {
		return nil, display.ErrNoAXPermission
	}
	root, maxDepth, ok := axRoot(target)
	if !ok {
		return nil, fmt.Errorf("no accessibility tree available for target %q (no frontmost application?)", target)
	}
	defer cfRelease(root)

	var elements []agentdomain.AnnotatedElement
	walkCollect(root, 0, maxDepth, &elements)
	return elements, nil
}

func (axProvider) PressElement(_ context.Context, target, label string) error {
	if err := axEnsureLoaded(); err != nil {
		return err
	}
	pool := objcAutoreleasePoolPush()
	defer objcAutoreleasePoolPop(pool)

	if !axIsProcessTrusted() {
		return display.ErrNoAXPermission
	}
	root, maxDepth, ok := axRoot(target)
	if !ok {
		return fmt.Errorf("no accessibility tree available for target %q (no frontmost application?)", target)
	}
	defer cfRelease(root)

	found, axErr := pressFirst(root, label, 0, maxDepth)
	switch {
	case !found:
		return fmt.Errorf("no element titled %q in %q: %w", label, target, display.ErrElementNotFound)
	case axErr != axErrorSuccess:
		return fmt.Errorf("press action failed for element %q in %q", label, target)
	default:
		return nil
	}
}

func init() {
	display.RegisterAXProvider(axProvider{})
	logger.Debug("registered macOS accessibility provider (purego)")
}
