package infrastructure

import (
	"encoding/json"
	"strings"
	"time"

	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// Frame names of the browser wire contract, as documented in
// docs/browser-extension-protocol.md.
const (
	frameBrowserHello           = "browser_hello"
	frameBrowserHelloAck        = "browser_hello_ack"
	frameBrowserCommand         = "browser_command"
	frameBrowserResult          = "browser_result"
	frameBrowserExtensionStatus = "browser_extension_status"
)

// statusProtocolVersion is the browser_extension_status frame's own schema
// version, independent of the handshake's, bumped when its fields change.
const statusProtocolVersion = 1

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

// browserExtensionStatusFrame builds the status frame a host reports to every
// other client when the extension attaches or detaches, and once right after a
// client joins. The version travels only while an extension is attached.
func browserExtensionStatusFrame(connected bool, version string) []byte {
	data, _ := json.Marshal(struct {
		Type             string `json:"type"`
		Connected        bool   `json:"connected"`
		ExtensionVersion string `json:"extension_version,omitempty"`
		ProtocolVersion  int    `json:"protocol_version"`
	}{
		Type:             frameBrowserExtensionStatus,
		Connected:        connected,
		ExtensionVersion: version,
		ProtocolVersion:  statusProtocolVersion,
	})
	return data
}
