package infrastructure

import (
	"strings"
	"time"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// Frame names of the browser wire contract, as documented in
// docs/browser-extension-protocol.md.
const (
	frameBrowserHello    = "browser_hello"
	frameBrowserHelloAck = "browser_hello_ack"
	frameBrowserCommand  = "browser_command"
	frameBrowserResult   = "browser_result"
)

// Client kinds a hello declares. A browser client only speaks browser frames:
// it reaches the extension through the host of the binding.
const (
	clientDesktop = "desktop"
	clientBrowser = "browser"
)

// commandReplyMargin is added to a command's own budget, which the extension
// must enforce itself, so a just-in-time answer still arrives.
const commandReplyMargin = 5 * time.Second

// BindingHandshake is the handshake the opentask extension and the other
// clients of its binding perform.
var BindingHandshake = agui.Handshake{Hello: frameBrowserHello, Ack: frameBrowserHelloAck}

// AllowExtensionOrigin accepts the origins a browser extension connects from.
func AllowExtensionOrigin(origin string) bool {
	return strings.HasPrefix(origin, "chrome-extension://") ||
		strings.HasPrefix(origin, "moz-extension://") ||
		strings.HasPrefix(origin, "safari-web-extension://")
}

// isExtension reports whether a client kind is the extension, which is what
// hellos without a known kind come from.
func isExtension(kind string) bool {
	return kind != clientDesktop && kind != clientBrowser
}
