//go:build darwin

package accessibility

import (
	"bytes"
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unsafe"

	purego "github.com/ebitengine/purego"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

const (
	cfStringEncodingUTF8            = 0x08000100
	cfNumberSInt32Type              = 3
	cfNumberSInt64Type              = 4
	axValueCGPointType              = 1
	axValueCGSizeType               = 2
	axErrorSuccess                  = 0
	maxTreeDepth                    = 12
	maxTreeElements                 = 250
	cgWindowListOptionOnScreenOnly  = 1 << 0
	cgWindowListExcludeDesktopItems = 1 << 4
)

type cgPoint struct{ X, Y float64 }
type cgSize struct{ Width, Height float64 }

type bridge struct {
	cfRelease                 func(uintptr)
	cfGetTypeID               func(uintptr) uintptr
	cfStringGetTypeID         func() uintptr
	cfArrayGetTypeID          func() uintptr
	cfBooleanGetTypeID        func() uintptr
	cfNumberGetTypeID         func() uintptr
	cfStringCreateWithCString func(uintptr, *byte, uint32) uintptr
	cfStringGetLength         func(uintptr) int
	cfStringGetCString        func(uintptr, *byte, int, uint32) bool
	cfArrayGetCount           func(uintptr) int
	cfArrayGetValueAtIndex    func(uintptr, int) uintptr
	cfBooleanGetValue         func(uintptr) bool
	cfNumberGetValue          func(uintptr, int32, unsafe.Pointer) bool
	cfDictionaryGetValue      func(uintptr, uintptr) uintptr

	axIsProcessTrusted         func() bool
	axCreateApplication        func(int32) uintptr
	axCreateSystemWide         func() uintptr
	axCopyAttributeValue       func(uintptr, uintptr, *uintptr) int32
	axCopyActionNames          func(uintptr, *uintptr) int32
	axPerformAction            func(uintptr, uintptr) int32
	axValueGetValue            func(uintptr, uint32, unsafe.Pointer) bool
	axSetMessagingTimeout      func(uintptr, float32) int32
	cgWindowListCopyWindowInfo func(uint32, uint32) uintptr

	attrs       map[string]uintptr
	ownerName   uintptr
	ownerPID    uintptr
	windowLayer uintptr
}

func runNative(req request) ([]computerdomain.UIElement, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	b, err := newBridge()
	if err != nil {
		return nil, err
	}
	defer b.close()
	if !b.axIsProcessTrusted() {
		return nil, ErrPermission
	}

	root, depth, err := b.resolveRoot(req.Target)
	if err != nil {
		return nil, err
	}
	defer b.cfRelease(root)
	_ = b.axSetMessagingTimeout(root, 3)

	switch req.Action {
	case "elements":
		return b.collect(root, depth), nil
	case "press":
		if req.Label == "" {
			return nil, fmt.Errorf("%w: label is empty", ErrElementNotFound)
		}
		found, code := b.pressFirst(root, req.Label, 0, depth, map[uintptr]bool{})
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrElementNotFound, req.Label)
		}
		if code != axErrorSuccess {
			return nil, fmt.Errorf("%w: AXPress returned %d", ErrUnavailable, code)
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: unknown helper action %q", ErrUnavailable, req.Action)
	}
}

func newBridge() (*bridge, error) {
	coreFoundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreFoundation: %w", err)
	}
	applicationServices, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load ApplicationServices: %w", err)
	}
	coreGraphics, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreGraphics: %w", err)
	}

	b := &bridge{attrs: make(map[string]uintptr)}
	if err := b.bindCoreFoundation(coreFoundation); err != nil {
		return nil, err
	}
	if err := b.bindAccessibility(applicationServices); err != nil {
		return nil, err
	}
	if err := b.bindCoreGraphics(coreGraphics); err != nil {
		return nil, err
	}
	if err := b.createConstants(); err != nil {
		b.close()
		return nil, err
	}
	return b, nil
}

func (b *bridge) bindCoreFoundation(handle uintptr) error {
	return bindFunctions(handle, []binding{
		{"CFRelease", &b.cfRelease},
		{"CFGetTypeID", &b.cfGetTypeID},
		{"CFStringGetTypeID", &b.cfStringGetTypeID},
		{"CFArrayGetTypeID", &b.cfArrayGetTypeID},
		{"CFBooleanGetTypeID", &b.cfBooleanGetTypeID},
		{"CFNumberGetTypeID", &b.cfNumberGetTypeID},
		{"CFStringCreateWithCString", &b.cfStringCreateWithCString},
		{"CFStringGetLength", &b.cfStringGetLength},
		{"CFStringGetCString", &b.cfStringGetCString},
		{"CFArrayGetCount", &b.cfArrayGetCount},
		{"CFArrayGetValueAtIndex", &b.cfArrayGetValueAtIndex},
		{"CFBooleanGetValue", &b.cfBooleanGetValue},
		{"CFNumberGetValue", &b.cfNumberGetValue},
		{"CFDictionaryGetValue", &b.cfDictionaryGetValue},
	})
}

func (b *bridge) bindAccessibility(handle uintptr) error {
	return bindFunctions(handle, []binding{
		{"AXIsProcessTrusted", &b.axIsProcessTrusted},
		{"AXUIElementCreateApplication", &b.axCreateApplication},
		{"AXUIElementCreateSystemWide", &b.axCreateSystemWide},
		{"AXUIElementCopyAttributeValue", &b.axCopyAttributeValue},
		{"AXUIElementCopyActionNames", &b.axCopyActionNames},
		{"AXUIElementPerformAction", &b.axPerformAction},
		{"AXValueGetValue", &b.axValueGetValue},
		{"AXUIElementSetMessagingTimeout", &b.axSetMessagingTimeout},
	})
}

func (b *bridge) bindCoreGraphics(handle uintptr) error {
	if err := bindFunctions(handle, []binding{{"CGWindowListCopyWindowInfo", &b.cgWindowListCopyWindowInfo}}); err != nil {
		return err
	}
	var err error
	b.ownerName, err = dataSymbol(handle, "kCGWindowOwnerName")
	if err != nil {
		return err
	}
	b.ownerPID, err = dataSymbol(handle, "kCGWindowOwnerPID")
	if err != nil {
		return err
	}
	b.windowLayer, err = dataSymbol(handle, "kCGWindowLayer")
	return err
}

type binding struct {
	name   string
	target any
}

func bindFunctions(handle uintptr, bindings []binding) error {
	for _, item := range bindings {
		symbol, err := purego.Dlsym(handle, item.name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", item.name, err)
		}
		purego.RegisterFunc(item.target, symbol)
	}
	return nil
}

// dataSymbol resolves a data symbol (e.g. a CFStringRef constant) and returns
// the pointer value stored at its address. Dlsym yields the symbol's absolute
// address as a uintptr; unsafe.Add(nil, addr) forms the equivalent
// unsafe.Pointer without a uintptr->Pointer conversion, which go vet's
// unsafeptr check flags even though the address is a stable framework symbol.
func dataSymbol(handle uintptr, name string) (uintptr, error) {
	symbol, err := purego.Dlsym(handle, name)
	if err != nil {
		return 0, fmt.Errorf("resolve %s: %w", name, err)
	}
	value := *(*uintptr)(unsafe.Add(nil, symbol))
	if value == 0 {
		return 0, fmt.Errorf("resolve %s: null value", name)
	}
	return value, nil
}

func (b *bridge) createConstants() error {
	for _, name := range []string{
		"AXRole", "AXTitle", "AXDescription", "AXHelp", "AXIdentifier", "AXPlaceholderValue",
		"AXPosition", "AXSize", "AXChildren", "AXMenuBar", "AXFocusedApplication", "AXEnabled",
		"AXFocused", "AXSelected", "AXValue", "AXPress",
	} {
		value := b.newString(name)
		if value == 0 {
			return fmt.Errorf("create accessibility constant %q", name)
		}
		b.attrs[name] = value
	}
	return nil
}

func (b *bridge) close() {
	if b.cfRelease == nil {
		return
	}
	for _, value := range b.attrs {
		if value != 0 {
			b.cfRelease(value)
		}
	}
	b.attrs = nil
}

func (b *bridge) newString(value string) uintptr {
	data := append([]byte(value), 0)
	return b.cfStringCreateWithCString(0, &data[0], cfStringEncodingUTF8)
}

func (b *bridge) goString(value uintptr) string {
	if value == 0 || b.cfGetTypeID(value) != b.cfStringGetTypeID() {
		return ""
	}
	length := b.cfStringGetLength(value)
	if length <= 0 {
		return ""
	}
	buffer := make([]byte, length*4+1)
	if !b.cfStringGetCString(value, &buffer[0], len(buffer), cfStringEncodingUTF8) {
		return ""
	}
	if end := bytes.IndexByte(buffer, 0); end >= 0 {
		buffer = buffer[:end]
	}
	return string(buffer)
}

func (b *bridge) copyAttribute(element uintptr, name string) (uintptr, bool) {
	var value uintptr
	if b.axCopyAttributeValue(element, b.attrs[name], &value) != axErrorSuccess || value == 0 {
		return 0, false
	}
	return value, true
}

func (b *bridge) stringAttribute(element uintptr, names ...string) string {
	for _, name := range names {
		value, ok := b.copyAttribute(element, name)
		if !ok {
			continue
		}
		text := b.goString(value)
		b.cfRelease(value)
		if text != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func (b *bridge) boolAttribute(element uintptr, name string) (bool, bool) {
	value, ok := b.copyAttribute(element, name)
	if !ok {
		return false, false
	}
	defer b.cfRelease(value)
	if b.cfGetTypeID(value) != b.cfBooleanGetTypeID() {
		return false, false
	}
	return b.cfBooleanGetValue(value), true
}

func (b *bridge) scalarAttribute(element uintptr, name string) string {
	value, ok := b.copyAttribute(element, name)
	if !ok {
		return ""
	}
	defer b.cfRelease(value)
	switch b.cfGetTypeID(value) {
	case b.cfStringGetTypeID():
		return truncate(b.goString(value), 80)
	case b.cfBooleanGetTypeID():
		return strconv.FormatBool(b.cfBooleanGetValue(value))
	case b.cfNumberGetTypeID():
		var number int64
		if b.cfNumberGetValue(value, cfNumberSInt64Type, unsafe.Pointer(&number)) {
			return strconv.FormatInt(number, 10)
		}
	}
	return ""
}

func (b *bridge) frame(element uintptr) ([4]int, bool) {
	position, ok := b.copyAttribute(element, "AXPosition")
	if !ok {
		return [4]int{}, false
	}
	defer b.cfRelease(position)
	var point cgPoint
	if !b.axValueGetValue(position, axValueCGPointType, unsafe.Pointer(&point)) {
		return [4]int{}, false
	}
	sizeValue, ok := b.copyAttribute(element, "AXSize")
	if !ok {
		return [4]int{}, false
	}
	defer b.cfRelease(sizeValue)
	var size cgSize
	if !b.axValueGetValue(sizeValue, axValueCGSizeType, unsafe.Pointer(&size)) || size.Width <= 0 || size.Height <= 0 {
		return [4]int{}, false
	}
	return [4]int{
		int(math.Round(point.X)),
		int(math.Round(point.Y)),
		int(math.Round(point.X + size.Width)),
		int(math.Round(point.Y + size.Height)),
	}, true
}

func (b *bridge) actionNames(element uintptr) []string {
	var actions uintptr
	if b.axCopyActionNames(element, &actions) != axErrorSuccess || actions == 0 {
		return nil
	}
	defer b.cfRelease(actions)
	if b.cfGetTypeID(actions) != b.cfArrayGetTypeID() {
		return nil
	}
	result := make([]string, 0, b.cfArrayGetCount(actions))
	for i := range b.cfArrayGetCount(actions) {
		if name := b.goString(b.cfArrayGetValueAtIndex(actions, i)); name != "" {
			result = append(result, normalizeAXName(name))
		}
	}
	return result
}

func (b *bridge) state(element uintptr, role string, actions []string) string {
	parts := make([]string, 0, 5)
	if enabled, ok := b.boolAttribute(element, "AXEnabled"); ok {
		if enabled {
			parts = append(parts, "enabled")
		} else {
			parts = append(parts, "disabled")
		}
	}
	if focused, _ := b.boolAttribute(element, "AXFocused"); focused {
		parts = append(parts, "focused")
	}
	if selected, _ := b.boolAttribute(element, "AXSelected"); selected {
		parts = append(parts, "selected")
	}
	if value := b.scalarAttribute(element, "AXValue"); value != "" && role != "secure text field" {
		parts = append(parts, "value="+value)
	}
	if len(actions) > 0 {
		parts = append(parts, "actions="+strings.Join(actions, ","))
	}
	return strings.Join(parts, " ")
}

func (b *bridge) compactElement(element uintptr) (computerdomain.UIElement, bool) {
	role := normalizeAXName(b.stringAttribute(element, "AXRole"))
	if role == "" {
		return computerdomain.UIElement{}, false
	}
	box, ok := b.frame(element)
	if !ok {
		return computerdomain.UIElement{}, false
	}
	label := b.stringAttribute(element, "AXTitle", "AXDescription", "AXPlaceholderValue", "AXHelp", "AXIdentifier")
	actions := b.actionNames(element)
	if label == "" && len(actions) == 0 {
		return computerdomain.UIElement{}, false
	}
	return computerdomain.UIElement{Role: role, Label: label, State: b.state(element, role, actions), BBox: box}, true
}

func (b *bridge) collect(root uintptr, maxDepth int) []computerdomain.UIElement {
	elements := make([]computerdomain.UIElement, 0, 64)
	b.walk(root, 0, maxDepth, map[uintptr]bool{}, &elements)
	return elements
}

func (b *bridge) walk(element uintptr, depth, maxDepth int, seen map[uintptr]bool, elements *[]computerdomain.UIElement) {
	if element == 0 || depth > maxDepth || len(*elements) >= maxTreeElements || seen[element] {
		return
	}
	seen[element] = true
	if compact, ok := b.compactElement(element); ok {
		*elements = append(*elements, compact)
	}
	b.forEachChild(element, func(child uintptr) bool {
		b.walk(child, depth+1, maxDepth, seen, elements)
		return len(*elements) < maxTreeElements
	})
}

func (b *bridge) forEachChild(element uintptr, visit func(uintptr) bool) {
	children, ok := b.copyAttribute(element, "AXChildren")
	if !ok {
		return
	}
	defer b.cfRelease(children)
	if b.cfGetTypeID(children) != b.cfArrayGetTypeID() {
		return
	}
	for i := range b.cfArrayGetCount(children) {
		if !visit(b.cfArrayGetValueAtIndex(children, i)) {
			return
		}
	}
}

func (b *bridge) pressFirst(element uintptr, label string, depth, maxDepth int, seen map[uintptr]bool) (bool, int32) {
	if element == 0 || depth > maxDepth || seen[element] {
		return false, axErrorSuccess
	}
	seen[element] = true
	if b.stringAttribute(element, "AXTitle", "AXDescription", "AXPlaceholderValue", "AXHelp", "AXIdentifier") == label &&
		contains(b.actionNames(element), "press") {
		return true, b.axPerformAction(element, b.attrs["AXPress"])
	}
	var found bool
	var code int32
	b.forEachChild(element, func(child uintptr) bool {
		found, code = b.pressFirst(child, label, depth+1, maxDepth, seen)
		return !found
	})
	return found, code
}

func (b *bridge) resolveRoot(target string) (uintptr, int, error) {
	target = strings.TrimSpace(target)
	switch target {
	case "", "frontmost":
		root, err := b.frontmostApplication()
		return root, maxTreeDepth, err
	case "menubar":
		application, err := b.frontmostApplication()
		if err != nil {
			return 0, 0, err
		}
		defer b.cfRelease(application)
		menu, ok := b.copyAttribute(application, "AXMenuBar")
		if !ok {
			return 0, 0, fmt.Errorf("%w: frontmost application has no menu bar", ErrUnavailable)
		}
		return menu, 4, nil
	case "dock":
		return b.applicationNamed("Dock")
	}
	if value, ok := strings.CutPrefix(target, "pid:"); ok {
		pid, err := strconv.ParseInt(value, 10, 32)
		if err != nil || pid <= 0 {
			return 0, 0, fmt.Errorf("%w: invalid target %q", ErrUnavailable, target)
		}
		return b.axCreateApplication(int32(pid)), maxTreeDepth, nil
	}
	name, _ := strings.CutPrefix(target, "app:")
	return b.applicationNamed(strings.TrimSpace(name))
}

func (b *bridge) frontmostApplication() (uintptr, error) {
	system := b.axCreateSystemWide()
	if system == 0 {
		return 0, fmt.Errorf("%w: create system-wide accessibility element", ErrUnavailable)
	}
	defer b.cfRelease(system)
	application, ok := b.copyAttribute(system, "AXFocusedApplication")
	if ok {
		return application, nil
	}
	if pid := b.pidForFrontmostApplication(); pid != 0 {
		return b.axCreateApplication(pid), nil
	}
	return 0, fmt.Errorf("%w: no focused application or frontmost window", ErrUnavailable)
}

func (b *bridge) applicationNamed(name string) (uintptr, int, error) {
	if name == "" {
		return 0, 0, fmt.Errorf("%w: application name is empty", ErrUnavailable)
	}
	pid := b.pidForApplication(name)
	if pid == 0 {
		return 0, 0, fmt.Errorf("%w: no visible application named %q", ErrUnavailable, name)
	}
	return b.axCreateApplication(pid), maxTreeDepth, nil
}

func (b *bridge) pidForApplication(name string) int32 {
	options := uint32(cgWindowListOptionOnScreenOnly | cgWindowListExcludeDesktopItems)
	windows := b.cgWindowListCopyWindowInfo(options, 0)
	if windows == 0 {
		return 0
	}
	defer b.cfRelease(windows)
	for i := range b.cfArrayGetCount(windows) {
		window := b.cfArrayGetValueAtIndex(windows, i)
		owner := b.cfDictionaryGetValue(window, b.ownerName)
		if !applicationNamesMatch(b.goString(owner), name) {
			continue
		}
		if pid := b.windowPID(window); pid != 0 {
			return pid
		}
	}
	return 0
}

func (b *bridge) pidForFrontmostApplication() int32 {
	options := uint32(cgWindowListOptionOnScreenOnly | cgWindowListExcludeDesktopItems)
	windows := b.cgWindowListCopyWindowInfo(options, 0)
	if windows == 0 {
		return 0
	}
	defer b.cfRelease(windows)
	for i := range b.cfArrayGetCount(windows) {
		window := b.cfArrayGetValueAtIndex(windows, i)
		layerValue := b.cfDictionaryGetValue(window, b.windowLayer)
		var layer int32
		if layerValue == 0 || !b.cfNumberGetValue(layerValue, cfNumberSInt32Type, unsafe.Pointer(&layer)) || layer != 0 {
			continue
		}
		if pid := b.windowPID(window); pid != 0 {
			return pid
		}
	}
	return 0
}

func (b *bridge) windowPID(window uintptr) int32 {
	pidValue := b.cfDictionaryGetValue(window, b.ownerPID)
	var pid int32
	if pidValue == 0 || !b.cfNumberGetValue(pidValue, cfNumberSInt32Type, unsafe.Pointer(&pid)) {
		return 0
	}
	return pid
}

func applicationNamesMatch(owner, requested string) bool {
	if strings.EqualFold(strings.TrimSpace(owner), strings.TrimSpace(requested)) {
		return true
	}
	normalizedRequested := normalizeApplicationName(requested)
	return normalizedRequested != "" && normalizeApplicationName(owner) == normalizedRequested
}

func normalizeApplicationName(name string) string {
	var normalized strings.Builder
	for _, char := range name {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(unicode.ToLower(char))
		}
	}
	return normalized.String()
}

func normalizeAXName(value string) string {
	value = strings.TrimPrefix(value, "AX")
	var result strings.Builder
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte(' ')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
