//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#include <stdlib.h>
#include <string.h>
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>

// Accessibility (AXUIElement) bridge. All string-returning functions return
// strdup'd memory that the Go caller must free.

static NSString* axEscape(NSString *s) {
    s = [s stringByReplacingOccurrencesOfString:@"|" withString:@"_"];
    return [s stringByReplacingOccurrencesOfString:@"\n" withString:@" "];
}

static NSString* axStringAttr(AXUIElementRef el, CFStringRef attr) {
    CFTypeRef val = NULL;
    if (AXUIElementCopyAttributeValue(el, attr, &val) != kAXErrorSuccess || val == NULL) {
        return nil;
    }
    if (CFGetTypeID(val) != CFStringGetTypeID()) {
        CFRelease(val);
        return nil;
    }
    NSString *s = [NSString stringWithString:(__bridge NSString*)val];
    CFRelease(val);
    return s;
}

// Element title: AXTitle, falling back to AXDescription (toolbar buttons and
// dock items often only set the latter).
static NSString* axElementTitle(AXUIElementRef el) {
    NSString *t = axStringAttr(el, kAXTitleAttribute);
    if (t == nil || [t length] == 0) {
        t = axStringAttr(el, kAXDescriptionAttribute);
    }
    return t;
}

static bool axFrame(AXUIElementRef el, CGPoint *pos, CGSize *size) {
    CFTypeRef v = NULL;
    if (AXUIElementCopyAttributeValue(el, kAXPositionAttribute, &v) != kAXErrorSuccess || v == NULL) {
        return false;
    }
    bool ok = AXValueGetValue((AXValueRef)v, kAXValueTypeCGPoint, pos);
    CFRelease(v);
    if (!ok) {
        return false;
    }
    v = NULL;
    if (AXUIElementCopyAttributeValue(el, kAXSizeAttribute, &v) != kAXErrorSuccess || v == NULL) {
        return false;
    }
    ok = AXValueGetValue((AXValueRef)v, kAXValueTypeCGSize, size);
    CFRelease(v);
    return ok;
}

static bool axPressable(AXUIElementRef el) {
    CFArrayRef actions = NULL;
    if (AXUIElementCopyActionNames(el, &actions) != kAXErrorSuccess || actions == NULL) {
        return false;
    }
    bool found = CFArrayContainsValue(actions, CFRangeMake(0, CFArrayGetCount(actions)), kAXPressAction);
    CFRelease(actions);
    return found;
}

// axRoot resolves the tree root for a target and sets the walk depth.
// Returns a retained ref the caller must CFRelease, or NULL.
static AXUIElementRef axRoot(const char *target, int *maxDepth) {
    NSString *t = [NSString stringWithUTF8String:target];
    *maxDepth = 8;
    if ([t isEqualToString:@"dock"]) {
        NSArray *docks = [NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.apple.dock"];
        if ([docks count] == 0) {
            return NULL;
        }
        return AXUIElementCreateApplication([[docks firstObject] processIdentifier]);
    }
    NSRunningApplication *front = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (front == nil) {
        return NULL;
    }
    AXUIElementRef appEl = AXUIElementCreateApplication([front processIdentifier]);
    if ([t isEqualToString:@"menubar"]) {
        // Only the top-level menu titles are meaningful while menus are
        // closed, so the walk stays one level deep.
        CFTypeRef mb = NULL;
        AXUIElementCopyAttributeValue(appEl, kAXMenuBarAttribute, &mb);
        CFRelease(appEl);
        *maxDepth = 1;
        return (AXUIElementRef)mb;
    }
    return appEl;
}

// axWalk traverses the tree. In list mode (pressTitle == NULL) it appends
// "role|title|x|y|w|h\n" lines to out, capped at 200 elements. In press mode
// it retains the first titled pressable element matching pressTitle into
// *pressTarget and stops.
// ponytail: fixed caps (depth 8, 200 elements); make configurable if real apps overflow.
static void axWalk(AXUIElementRef el, int depth, int maxDepth, int *count,
                   NSMutableString *out, NSString *pressTitle, AXUIElementRef *pressTarget) {
    if (depth > maxDepth || (out != nil && *count >= 200)) {
        return;
    }
    if (pressTarget != NULL && *pressTarget != NULL) {
        return;
    }
    if (axPressable(el)) {
        NSString *title = axElementTitle(el);
        if (title != nil && [title length] > 0) {
            NSString *safe = axEscape(title);
            if (pressTitle != nil) {
                if ([safe isEqualToString:pressTitle]) {
                    *pressTarget = (AXUIElementRef)CFRetain(el);
                    return;
                }
            } else {
                CGPoint pos;
                CGSize size;
                if (axFrame(el, &pos, &size)) {
                    NSString *role = axStringAttr(el, kAXRoleAttribute);
                    if (role == nil) role = @"AXUnknown";
                    [out appendFormat:@"%@|%@|%d|%d|%d|%d\n", role, safe,
                        (int)pos.x, (int)pos.y, (int)size.width, (int)size.height];
                    (*count)++;
                }
            }
        }
    }
    CFTypeRef childrenRef = NULL;
    if (AXUIElementCopyAttributeValue(el, kAXChildrenAttribute, &childrenRef) != kAXErrorSuccess || childrenRef == NULL) {
        return;
    }
    if (CFGetTypeID(childrenRef) == CFArrayGetTypeID()) {
        CFArrayRef children = (CFArrayRef)childrenRef;
        CFIndex n = CFArrayGetCount(children);
        for (CFIndex i = 0; i < n; i++) {
            axWalk((AXUIElementRef)CFArrayGetValueAtIndex(children, i),
                   depth + 1, maxDepth, count, out, pressTitle, pressTarget);
            if (pressTarget != NULL && *pressTarget != NULL) {
                break;
            }
        }
    }
    CFRelease(childrenRef);
}

// axListElements returns "role|title|x|y|w|h\n" lines for target's pressable
// elements, "ERR:permission" when accessibility is not granted, or
// "ERR:notarget" when the tree root cannot be resolved.
char* axListElements(const char* target) {
    @autoreleasepool {
        if (!AXIsProcessTrusted()) {
            return strdup("ERR:permission");
        }
        int maxDepth = 0;
        AXUIElementRef root = axRoot(target, &maxDepth);
        if (root == NULL) {
            return strdup("ERR:notarget");
        }
        NSMutableString *out = [NSMutableString string];
        int count = 0;
        axWalk(root, 0, maxDepth, &count, out, NULL, NULL);
        CFRelease(root);
        return strdup([out UTF8String]);
    }
}

// axPressElement presses the first element titled `title` in target's tree.
// Returns 0 on success, 1 = no permission, 2 = no target root,
// 3 = element not found, 4 = press action failed.
int axPressElement(const char* target, const char* title) {
    @autoreleasepool {
        if (!AXIsProcessTrusted()) {
            return 1;
        }
        int maxDepth = 0;
        AXUIElementRef root = axRoot(target, &maxDepth);
        if (root == NULL) {
            return 2;
        }
        NSString *wanted = [NSString stringWithUTF8String:title];
        AXUIElementRef match = NULL;
        int count = 0;
        axWalk(root, 0, maxDepth, &count, nil, wanted, &match);
        CFRelease(root);
        if (match == NULL) {
            return 3;
        }
        AXError err = AXUIElementPerformAction(match, kAXPressAction);
        CFRelease(match);
        return err == kAXErrorSuccess ? 0 : 4;
    }
}
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// axProvider implements display.AXProvider via the AXUIElement cgo bridge
// above. Requires the Accessibility (TCC) permission; without it every call
// returns display.ErrNoAXPermission.
type axProvider struct{}

var _ display.AXProvider = (*axProvider)(nil)

func (axProvider) ListElements(ctx context.Context, target string) ([]domain.AnnotatedElement, error) {
	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))

	cs := C.axListElements(cTarget)
	defer C.free(unsafe.Pointer(cs))

	s := C.GoString(cs)
	switch s {
	case "ERR:permission":
		return nil, display.ErrNoAXPermission
	case "ERR:notarget":
		return nil, fmt.Errorf("no accessibility tree available for target %q (no frontmost application?)", target)
	}
	return parseAXElements(s), nil
}

func (axProvider) PressElement(ctx context.Context, target, label string) error {
	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))

	switch C.axPressElement(cTarget, cLabel) {
	case 0:
		return nil
	case 1:
		return display.ErrNoAXPermission
	case 2:
		return fmt.Errorf("no accessibility tree available for target %q (no frontmost application?)", target)
	case 3:
		return fmt.Errorf("no element titled %q in %q: %w", label, target, display.ErrElementNotFound)
	default:
		return fmt.Errorf("press action failed for element %q in %q", label, target)
	}
}

func init() {
	display.RegisterAXProvider(axProvider{})
	logger.Debug("registered macOS accessibility provider")
}
