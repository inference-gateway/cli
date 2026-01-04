//go:build darwin

package macos

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// FloatingWindowManager manages the lifecycle of the floating progress window
type FloatingWindowManager struct {
	cfg          *config.Config
	eventBridge  *EventBridge
	stateManager domain.StateManager
	cmd          *exec.Cmd
	enabled      bool
	eventSub     chan domain.ChatEvent
	stopForward  chan struct{}
	swiftTmpFile string
	monitorWg    sync.WaitGroup
	// IPC fields (merged from ProcessManager)
	stdin                io.Writer
	stdout               io.Reader
	stdinMutex           sync.Mutex
	approvalChans        map[string]chan domain.ApprovalAction
	approvalMutex        sync.RWMutex
	stopListener         chan struct{}
	listenerStopped      bool
	listenerStoppedMutex sync.Mutex
}

// NewFloatingWindowManager creates and starts a new floating window manager
func NewFloatingWindowManager(cfg *config.Config, eventBridge *EventBridge, stateManager domain.StateManager) (*FloatingWindowManager, error) {
	if runtime.GOOS != "darwin" {
		return &FloatingWindowManager{enabled: false}, nil
	}

	if !cfg.ComputerUse.Enabled || !cfg.ComputerUse.FloatingWindow.Enabled {
		return &FloatingWindowManager{enabled: false}, nil
	}

	mgr := &FloatingWindowManager{
		cfg:             cfg,
		eventBridge:     eventBridge,
		stateManager:    stateManager,
		enabled:         true,
		stopForward:     make(chan struct{}),
		approvalChans:   make(map[string]chan domain.ApprovalAction),
		stopListener:    make(chan struct{}),
		listenerStopped: false,
	}

	if err := mgr.launchWindow(); err != nil {
		return nil, fmt.Errorf("failed to launch floating window: %w", err)
	}

	mgr.eventSub = eventBridge.Subscribe()
	go mgr.forwardEvents()

	mgr.monitorWg.Add(1)
	go mgr.monitorProcess()

	return mgr, nil
}

// launchWindow starts the Swift window process
func (mgr *FloatingWindowManager) launchWindow() error {
	swiftScript := mgr.generateSwiftScript()

	tmpDir := mgr.cfg.GetConfigDir() + "/tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "floating_window_*.swift")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.Write([]byte(swiftScript)); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return fmt.Errorf("failed to write Swift script: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	mgr.swiftTmpFile = tmpFile.Name()

	cmd := exec.Command("swift", tmpFile.Name())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Swift process: %w", err)
	}

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				logger.Debug("Swift stderr", "output", string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	mgr.stdin = stdin
	mgr.stdout = stdout
	mgr.cmd = cmd

	go mgr.startApprovalListener()

	return nil
}

// forwardEvents forwards chat events from the EventBridge to the Swift window
func (mgr *FloatingWindowManager) forwardEvents() {
	for {
		select {
		case event := <-mgr.eventSub:
			if approvalEvent, ok := event.(domain.ToolApprovalRequestedEvent); ok {
				mgr.registerApprovalChannel(approvalEvent.ToolCall.Id, approvalEvent.ResponseChan)
			}

			if err := mgr.writeEvent(event); err != nil {
				logger.Warn("Failed to forward event to window", "error", err)
			}

		case <-mgr.stopForward:
			logger.Debug("Event forwarding stopped")
			return
		}
	}
}

// monitorProcess watches the Swift process and respawns if configured
func (mgr *FloatingWindowManager) monitorProcess() {
	defer mgr.monitorWg.Done()

	if mgr.cmd == nil {
		return
	}

	err := mgr.cmd.Wait()
	if err != nil {
		logger.Error("Swift process exited", "error", err)
	}

	if mgr.enabled && mgr.cfg.ComputerUse.FloatingWindow.RespawnOnClose {
		time.Sleep(1 * time.Second)

		if err := mgr.launchWindow(); err != nil {
			logger.Error("Failed to respawn floating window", "error", err)
			return
		}

		mgr.monitorWg.Add(1)
		go mgr.monitorProcess()
	}
}

// Shutdown gracefully shuts down the floating window manager
func (mgr *FloatingWindowManager) Shutdown() error {
	if !mgr.enabled {
		return nil
	}

	logger.Info("Shutting down floating window manager")

	mgr.enabled = false

	close(mgr.stopForward)

	if mgr.eventSub != nil {
		mgr.eventBridge.Unsubscribe(mgr.eventSub)
	}

	mgr.stopApprovalListener()

	if err := mgr.shutdownProcess(); err != nil {
		return err
	}

	mgr.monitorWg.Wait()

	if mgr.swiftTmpFile != "" {
		if err := os.Remove(mgr.swiftTmpFile); err != nil {
			logger.Debug("Failed to remove temp Swift file", "error", err, "path", mgr.swiftTmpFile)
		} else {
			logger.Debug("Removed temp Swift file", "path", mgr.swiftTmpFile)
		}
		mgr.swiftTmpFile = ""
	}

	logger.Info("Floating window manager shutdown complete")

	return nil
}

// shutdownProcess terminates the Swift process gracefully
func (mgr *FloatingWindowManager) shutdownProcess() error {
	if mgr.cmd == nil || mgr.cmd.Process == nil {
		return nil
	}

	return mgr.sendTermSignal()
}

// sendTermSignal sends SIGTERM to the Swift process, falls back to SIGKILL if needed
func (mgr *FloatingWindowManager) sendTermSignal() error {
	if err := mgr.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		logger.Debug("Failed to send SIGTERM, using SIGKILL", "error", err)
		if killErr := mgr.cmd.Process.Kill(); killErr != nil {
			logger.Warn("Failed to kill Swift process", "error", killErr)
			return fmt.Errorf("failed to kill process: %w", killErr)
		}
	}
	return nil
}

// IPC Methods (merged from ProcessManager)

// writeEvent sends an event to the Swift process via stdin
func (mgr *FloatingWindowManager) writeEvent(event domain.ChatEvent) error {
	mgr.stdinMutex.Lock()
	defer mgr.stdinMutex.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := fmt.Fprintf(mgr.stdin, "%s\n", data); err != nil {
		logger.Warn("Failed to write event to window", "error", err)
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// startApprovalListener reads approval responses from the Swift process via stdout
func (mgr *FloatingWindowManager) startApprovalListener() {
	scanner := bufio.NewScanner(mgr.stdout)
	for scanner.Scan() {
		select {
		case <-mgr.stopListener:
			logger.Debug("Approval listener stopped")
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			continue
		}

		var response ApprovalResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			logger.Warn("Failed to parse approval response", "error", err, "line", line)
			continue
		}

		mgr.handleApprovalResponse(response)
	}

	if err := scanner.Err(); err != nil {
		mgr.listenerStoppedMutex.Lock()
		if !mgr.listenerStopped {
			logger.Warn("Approval listener error", "error", err)
		}
		mgr.listenerStoppedMutex.Unlock()
	}
}

// handleApprovalResponse processes an approval response from the window
func (mgr *FloatingWindowManager) handleApprovalResponse(resp ApprovalResponse) {
	mgr.approvalMutex.Lock()
	defer mgr.approvalMutex.Unlock()

	logger.Debug("handleApprovalResponse called", "call_id", resp.CallID, "registered_channels", len(mgr.approvalChans))

	ch, exists := mgr.approvalChans[resp.CallID]
	if !exists {
		logger.Warn("Received approval for unknown call ID", "call_id", resp.CallID, "known_call_ids", mgr.getCallIDs())
		return
	}

	logger.Info("Sending approval to channel", "call_id", resp.CallID, "action", resp.Action)

	select {
	case ch <- resp.Action:
		delete(mgr.approvalChans, resp.CallID)
		logger.Debug("Approval processed", "call_id", resp.CallID, "action", resp.Action)

		if mgr.stateManager != nil {
			mgr.stateManager.ClearApprovalUIState()
			logger.Debug("Cleared approval UI state from floating window")
		}
	default:
		logger.Warn("Approval channel blocked", "call_id", resp.CallID)
	}
}

// registerApprovalChannel registers a response channel for a specific tool call
func (mgr *FloatingWindowManager) registerApprovalChannel(callID string, ch chan domain.ApprovalAction) {
	mgr.approvalMutex.Lock()
	defer mgr.approvalMutex.Unlock()

	mgr.approvalChans[callID] = ch
	logger.Debug("Registered approval channel", "call_id", callID)
}

// getCallIDs returns a list of registered call IDs (for debugging)
func (mgr *FloatingWindowManager) getCallIDs() []string {
	ids := make([]string, 0, len(mgr.approvalChans))
	for id := range mgr.approvalChans {
		ids = append(ids, id)
	}
	return ids
}

// stopApprovalListener signals the approval listener to stop
func (mgr *FloatingWindowManager) stopApprovalListener() {
	mgr.listenerStoppedMutex.Lock()
	defer mgr.listenerStoppedMutex.Unlock()

	if !mgr.listenerStopped {
		close(mgr.stopListener)
		mgr.listenerStopped = true
		logger.Debug("Approval listener stopped")
	}
}

// generateSwiftScript generates the Swift script for the floating window
//
//nolint:funlen // Swift script embedding requires long function
func (mgr *FloatingWindowManager) generateSwiftScript() string {
	position := mgr.cfg.ComputerUse.FloatingWindow.Position
	alwaysOnTop := mgr.cfg.ComputerUse.FloatingWindow.AlwaysOnTop

	return fmt.Sprintf(`
import Cocoa
import Foundation
import WebKit

// MARK: - Models

struct ApprovalResponse: Codable {
    let call_id: String
    let action: Int  // 0=Approve, 1=Reject, 2=AutoAccept
}

// MARK: - Window Setup

class AgentProgressWindow: NSPanel {
    let webView = WKWebView()
    var isTerminalReady = false
    var isMinimized = false
    var fullFrame: NSRect?
    let minimizedWidth: CGFloat = 40
    let minimizedHeight: CGFloat = 150

    init() {
        let screenFrame = NSScreen.main!.visibleFrame
        let windowWidth: CGFloat = 450
        let windowHeight: CGFloat = 600

        // Position based on configuration
        var xPos: CGFloat
        let position = "%s"
        switch position {
        case "top-left":
            xPos = screenFrame.minX + 20
        case "top-right":
            xPos = screenFrame.maxX - windowWidth - 20
        default:
            xPos = screenFrame.maxX - windowWidth - 20
        }

        let yPos = screenFrame.maxY - windowHeight - 20
        let frame = NSRect(x: xPos, y: yPos, width: windowWidth, height: windowHeight)

        let styleMask: NSWindow.StyleMask = [.titled, .resizable, .miniaturizable, .fullSizeContentView]
        super.init(contentRect: frame, styleMask: styleMask, backing: .buffered, defer: false)

        self.title = "Computer Use"
        self.isFloatingPanel = true
        self.level = %t ? .floating : .normal
        self.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        self.hidesOnDeactivate = false

        self.isOpaque = false
        self.alphaValue = 0.90
        self.backgroundColor = NSColor(calibratedWhite: 0.1, alpha: 0.85)

        self.contentView?.wantsLayer = true
        if let layer = self.contentView?.layer {
            layer.cornerRadius = 12
            layer.masksToBounds = false
        }

        self.hasShadow = true
        self.invalidateShadow()

        self.titlebarAppearsTransparent = true
        self.titleVisibility = .visible

        self.isMovableByWindowBackground = true

        self.standardWindowButton(.closeButton)?.alphaValue = 0
        self.standardWindowButton(.zoomButton)?.alphaValue = 0

        if let minimizeButton = self.standardWindowButton(.miniaturizeButton) {
            minimizeButton.target = self
            minimizeButton.action = #selector(customMinimize)
        }

        setupUI()

        self.orderFront(nil)
    }

    @objc func customMinimize() {
        if isMinimized {
            restoreWindow()
        } else {
            minimizeToSide()
        }
    }

    func minimizeToSide() {
        guard let screen = NSScreen.main else { return }
        isMinimized = true
        fullFrame = self.frame

        let screenFrame = screen.visibleFrame
        let xPos = screenFrame.maxX - minimizedWidth
        let yPos = screenFrame.midY - (minimizedHeight / 2)
        let minimizedFrame = NSRect(x: xPos, y: yPos, width: minimizedWidth, height: minimizedHeight)

        NSAnimationContext.runAnimationGroup({ context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            self.animator().setFrame(minimizedFrame, display: true)
            self.animator().alphaValue = 1.0
        }, completionHandler: {
            self.webView.isHidden = true
            self.titleVisibility = .hidden
            self.titlebarAppearsTransparent = true
            self.standardWindowButton(.closeButton)?.alphaValue = 0
            self.standardWindowButton(.miniaturizeButton)?.alphaValue = 0
            self.standardWindowButton(.zoomButton)?.alphaValue = 0
            self.isOpaque = true
            self.backgroundColor = NSColor(calibratedWhite: 0.1, alpha: 1.0)
            self.updateMinimizedUI()
        })
    }

    func restoreWindow() {
        guard let savedFrame = fullFrame else { return }
        isMinimized = false

        self.titleVisibility = .visible
        self.titlebarAppearsTransparent = true
        self.standardWindowButton(.closeButton)?.alphaValue = 0
        self.standardWindowButton(.miniaturizeButton)?.alphaValue = 1.0
        self.standardWindowButton(.zoomButton)?.alphaValue = 0
        self.isOpaque = false
        self.backgroundColor = NSColor(calibratedWhite: 0.1, alpha: 0.85)

        if let contentView = self.contentView {
            contentView.subviews.forEach { view in
                if view.identifier?.rawValue == "minimizedLabel" {
                    view.removeFromSuperview()
                }
            }
        }

        self.webView.isHidden = false

        NSAnimationContext.runAnimationGroup({ context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            self.animator().setFrame(savedFrame, display: true)
            self.animator().alphaValue = 0.95
        }, completionHandler: nil)
    }

    func updateMinimizedUI() {
        guard let contentView = self.contentView else { return }

        contentView.subviews.forEach { view in
            if view.identifier?.rawValue == "minimizedLabel" {
                view.removeFromSuperview()
            }
        }

        // Create a simple dot indicator, vertically centered
        let labelHeight: CGFloat = 30
        let labelY = (minimizedHeight - labelHeight) / 2
        let label = NSTextField(labelWithString: "●")
        label.identifier = NSUserInterfaceItemIdentifier("minimizedLabel")
        label.frame = NSRect(x: 0, y: labelY, width: minimizedWidth, height: labelHeight)
        label.alignment = .center
        label.font = NSFont.systemFont(ofSize: 20)
        label.textColor = NSColor(calibratedRed: 0.48, green: 0.64, blue: 0.97, alpha: 1.0)  // Blue accent color
        label.backgroundColor = .clear
        label.isBordered = false
        label.isEditable = false
        label.isSelectable = false
        contentView.addSubview(label)
    }

    override func mouseDown(with event: NSEvent) {
        super.mouseDown(with: event)
        if isMinimized {
            customMinimize()
        }
    }

    func setupUI() {
        guard let contentView = self.contentView else { return }

        // WebView leaves 30px at top for draggable title bar
        let titleBarHeight: CGFloat = 30
        webView.frame = NSRect(x: 0, y: 0, width: contentView.bounds.width, height: contentView.bounds.height - titleBarHeight)
        webView.autoresizingMask = [.width, .height]
        webView.setValue(false, forKey: "drawsBackground")

        let html = """
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="UTF-8">
            <style>
                body {
                    margin: 0;
                    padding: 0;
                    background: transparent;
                    overflow: hidden;
                    width: 100vw;
                    height: 100vh;
                }
                #terminal {
                    width: 100%%;
                    height: 100vh;
                    overflow: hidden;
                    background: #1a1b26;
                }
                .xterm {
                    padding: 12px;
                }
                #approvalBox {
                    position: fixed;
                    bottom: 10px;
                    left: 10px;
                    right: 10px;
                    max-width: calc(100vw - 20px);
                    background: #24283b;
                    border: 2px solid #7aa2f7;
                    border-radius: 8px;
                    padding: 12px;
                    display: none;
                    z-index: 1000;
                    box-sizing: border-box;
                }
                #approvalBox.visible { display: block; }
                #approvalBox .buttons {
                    display: flex;
                    gap: 8px;
                }
                #approvalBox button {
                    flex: 1;
                    padding: 8px 12px;
                    border: none;
                    border-radius: 4px;
                    cursor: pointer;
                    font-family: 'Menlo', monospace;
                    font-size: 12px;
                    font-weight: 600;
                }
                #approvalBox .approve {
                    background: #9ece6a;
                    color: #1a1b26;
                }
                #approvalBox .approve:hover { background: #b9f27c; }
                #approvalBox .reject {
                    background: #f7768e;
                    color: #1a1b26;
                }
                #approvalBox .reject:hover { background: #ff7a93; }
                #approvalBox .auto {
                    background: #7aa2f7;
                    color: #1a1b26;
                }
                #approvalBox .auto:hover { background: #7da6ff; }
            </style>
            <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/xterm@5.3.0/css/xterm.css" />
            <script src="https://cdn.jsdelivr.net/npm/xterm@5.3.0/lib/xterm.js"></script>
        </head>
        <body>
            <div id="terminal"></div>
            <div id="approvalBox">
                <div class="buttons">
                    <button class="approve" onclick="sendApproval(0)">✓ Approve</button>
                    <button class="reject" onclick="sendApproval(1)">✗ Reject</button>
                    <button class="auto" onclick="sendApproval(2)">Auto-Approve</button>
                </div>
            </div>
            <script>
                const term = new Terminal({
                    cols: 70,
                    rows: 40,
                    cursorBlink: false,
                    fontSize: 10.5,
                    fontFamily: 'Menlo, Monaco, monospace',
                    lineHeight: 1.2,
                    scrollback: 1000,
                    theme: {
                        background: '#1a1b26',
                        foreground: '#a9b1d6',
                        cursor: '#c0caf5',
                        black: '#32344a',
                        red: '#f7768e',
                        green: '#9ece6a',
                        yellow: '#e0af68',
                        blue: '#7aa2f7',
                        magenta: '#ad8ee6',
                        cyan: '#449dab',
                        white: '#787c99',
                        brightBlack: '#444b6a',
                        brightRed: '#ff7a93',
                        brightGreen: '#b9f27c',
                        brightYellow: '#ff9e64',
                        brightBlue: '#7da6ff',
                        brightMagenta: '#bb9af7',
                        brightCyan: '#0db9d7',
                        brightWhite: '#acb0d0'
                    }
                });
                term.open(document.getElementById('terminal'));
                window.term = term;
                window.currentCallID = null;

                window.showApproval = function(callID, toolName) {
                    window.currentCallID = callID;
                    document.getElementById('approvalBox').classList.add('visible');
                };

                window.sendApproval = function(action) {
                    if (window.currentCallID) {
                        window.webkit.messageHandlers.approval.postMessage({
                            call_id: window.currentCallID,
                            action: action
                        });
                        document.getElementById('approvalBox').classList.remove('visible');
                        window.currentCallID = null;
                    }
                };

                window.webkit.messageHandlers.terminalReady.postMessage('ready');
            </script>
        </body>
        </html>
        """

        let userController = webView.configuration.userContentController
        userController.add(self, name: "terminalReady")
        userController.add(self, name: "approval")

        let consoleScript = """
        console.log = function(msg) {
            window.webkit.messageHandlers.consoleLog.postMessage(String(msg));
        };
        """
        let consoleUserScript = WKUserScript(source: consoleScript, injectionTime: .atDocumentStart, forMainFrameOnly: true)
        userController.addUserScript(consoleUserScript)
        userController.add(self, name: "consoleLog")

        webView.loadHTMLString(html, baseURL: nil)
        contentView.addSubview(webView)
    }

    func escapeForJS(_ text: String) -> String {
        return text.replacingOccurrences(of: "\\", with: "\\\\")
                   .replacingOccurrences(of: "'", with: "\\'")
                   .replacingOccurrences(of: "\n", with: "\\n")
                   .replacingOccurrences(of: "\r", with: "")
    }

    func writeToTerminal(_ text: String) {
        guard isTerminalReady else { return }
        let escaped = escapeForJS(text)
        let js = "window.term.write('\(escaped)');"
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func writeLineToTerminal(_ text: String) {
        guard isTerminalReady else { return }
        let escaped = escapeForJS(text)
        let js = "window.term.writeln('\(escaped)');"
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func formatToolArguments(_ jsonString: String) -> String {
        guard let data = jsonString.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return jsonString.count > 100 ? String(jsonString.prefix(100)) + "..." : jsonString
        }

        var lines: [String] = []
        for (key, value) in json.sorted(by: { $0.key < $1.key }) {
            let valueStr: String
            if let str = value as? String {
                valueStr = str.count > 60 ? String(str.prefix(60)) + "..." : str
            } else if let num = value as? NSNumber {
                valueStr = "\(num)"
            } else {
                valueStr = "\(value)"
            }
            lines.append("\(key): \(valueStr)")
        }
        return lines.joined(separator: "\n")
    }

    func addEvent(type: String, description: String, callID: String? = nil, toolName: String? = nil, toolArgs: String? = nil) {
        DispatchQueue.main.async {
            let esc = "\u{001B}"
            let cyan = "\(esc)[36m"
            let yellow = "\(esc)[33m"
            let magenta = "\(esc)[35m"
            let gray = "\(esc)[90m"
            let reset = "\(esc)[0m"

            switch type {
            case "Chat Start":
                self.writeLineToTerminal("")
                self.writeLineToTerminal("\(cyan)●\(reset) Starting...")
                self.writeLineToTerminal("")
            case "Chat Chunk":
                self.writeToTerminal(description)
            case "Tool Approval":
                if let cid = callID, let tool = toolName {
                    self.showApprovalButtons(callID: cid, toolName: tool)
                }
            case "Tool Execution":
                if let tool = toolName {
                    let green = "\(esc)[32m"
                    let blue = "\(esc)[34m"
                    let bold = "\(esc)[1m"
                    let dim = "\(esc)[2m"

                    self.writeLineToTerminal("")
                    self.writeLineToTerminal("\(blue)▶\(reset) \(bold)\(tool)\(reset)")

                    // Format arguments nicely
                    if let args = toolArgs, !args.isEmpty && args != "{}" {
                        let formattedArgs = self.formatToolArguments(args)
                        for line in formattedArgs.split(separator: "\n") {
                            self.writeLineToTerminal("  \(dim)\(line)\(reset)")
                        }
                    }
                } else {
                    self.writeLineToTerminal("\(gray)  \(description)\(reset)")
                }
            case "Tool Failed", "Tool Rejected":
                let red = "\(esc)[31m"
                let bold = "\(esc)[1m"
                self.writeLineToTerminal("")
                self.writeLineToTerminal("\(red)✗ \(bold)\(description)\(reset)")
                self.writeLineToTerminal("")
            case "Approval Cleared":
                self.hideApprovalBox()
            case "Cancelled":
                let red = "\(esc)[31m"
                let bold = "\(esc)[1m"
                self.writeLineToTerminal("")
                self.writeLineToTerminal("\(red)✗ \(bold)\(description)\(reset)")
                self.writeLineToTerminal("")
            case "Event":
                // Skip generic events to reduce noise
                break
            case "Optimization":
                self.writeLineToTerminal("")
                self.writeLineToTerminal("\(magenta)⚡ \(description)\(reset)")
            default:
                self.writeLineToTerminal("\(gray)[\(type)] \(description)\(reset)")
            }
        }
    }

    func hideApprovalBox() {
        guard isTerminalReady else { return }
        let js = "document.getElementById('approvalBox').classList.remove('visible'); window.currentCallID = null;"
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func showApprovalButtons(callID: String, toolName: String) {
        guard isTerminalReady else {
            return
        }
        let escapedCallID = escapeForJS(callID)
        let escapedToolName = escapeForJS(toolName)
        let js = "window.showApproval('\(escapedCallID)', '\(escapedToolName)');"
        webView.evaluateJavaScript(js, completionHandler: nil)
    }

    func sendApproval(callID: String, action: Int) {
        let response = ApprovalResponse(call_id: callID, action: action)
        if let jsonData = try? JSONEncoder().encode(response),
           let jsonString = String(data: jsonData, encoding: .utf8) {
            print(jsonString)  // Send to stdout
            fflush(stdout)
        }
    }
}

// MARK: - WebKit Message Handler

extension AgentProgressWindow: WKScriptMessageHandler {
    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.name == "terminalReady" {
            isTerminalReady = true
            fputs("Terminal ready for output\n", stderr)
        } else if message.name == "approval",
                  let data = message.body as? [String: Any],
                  let callID = data["call_id"] as? String,
                  let action = data["action"] as? Int {
            fputs("Received approval from UI: callID=\(callID), action=\(action)\n", stderr)
            sendApproval(callID: callID, action: action)
        } else if message.name == "consoleLog" {
            fputs("JS console: \(message.body)\n", stderr)
        }
    }
}

// MARK: - Event Reading

class EventReader {
    let window: AgentProgressWindow

    init(window: AgentProgressWindow) {
        self.window = window
    }

    func startReading() {
        DispatchQueue.global(qos: .userInitiated).async {
            let handle = FileHandle.standardInput

            while true {
                var lineData = Data()

                while true {
                    do {
                        guard let byte = try handle.read(upToCount: 1), !byte.isEmpty else {
                            return
                        }

                        if byte[0] == 10 {
                            break
                        }
                        lineData.append(byte[0])
                    } catch {
                        fputs("Read error: \(error)\n", stderr)
                        return
                    }
                }

                if let line = String(data: lineData, encoding: .utf8), !line.isEmpty {
                    self.handleEvent(line)
                }
            }
        }
    }

    func handleEvent(_ jsonString: String) {
        guard let data = jsonString.data(using: .utf8) else {
            fputs("ERROR: Failed to convert string to data\n", stderr)
            return
        }

        do {
            if let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] {

                let eventType = self.extractEventType(from: json)
                let description = self.extractDescription(from: json)

                var callID: String? = nil
                var toolName: String? = nil
                var toolArgs: String? = nil

                if eventType == "Parallel Tools Start" {
                    if let tools = json["Tools"] as? [[String: Any]] {
                        for tool in tools {
                            let tName = tool["Name"] as? String
                            let tArgs = tool["Arguments"] as? String

                            DispatchQueue.main.async {
                                self.window.addEvent(type: "Tool Execution", description: "", callID: nil, toolName: tName, toolArgs: tArgs)
                            }
                        }
                    }
                } else if eventType == "Chat Complete" {
                    if let toolCalls = json["ToolCalls"] as? [[String: Any]] {
                        for toolCall in toolCalls {
                            if let function = toolCall["function"] as? [String: Any] {
                                let tName = function["name"] as? String
                                let tArgs = function["arguments"] as? String

                                DispatchQueue.main.async {
                                    self.window.addEvent(type: "Tool Execution", description: "", callID: nil, toolName: tName, toolArgs: tArgs)
                                }
                            }
                        }
                    }
                } else if eventType == "Tool Execution" {
                    toolName = json["ToolName"] as? String
                    toolArgs = json["Arguments"] as? String
                } else if eventType == "Tool Execution Progress" {
                    if let status = json["Status"] as? String, status == "failed" {
                        if let tName = json["ToolName"] as? String {
                            let failureMsg = "Tool: \(tName) failed"
                            DispatchQueue.main.async {
                                self.window.addEvent(type: "Tool Failed", description: failureMsg, callID: nil, toolName: nil, toolArgs: nil)
                            }
                        }
                    }
                    return
                } else if eventType == "Tool Approval" {
                    if let toolCall = json["ToolCall"] as? [String: Any] {
                        callID = toolCall["id"] as? String
                        if let function = toolCall["function"] as? [String: Any] {
                            toolName = function["name"] as? String
                        }
                    }
                }

                if eventType != "Chat Chunk" {
                    fputs("Parsed event: type=\(eventType), desc=\(description), callID=\(callID ?? "nil")\n", stderr)
                }

                // Only call addEvent for events we haven't already handled
                if eventType != "Parallel Tools Start" && eventType != "Chat Complete" {
                    DispatchQueue.main.async {
                        self.window.addEvent(type: eventType, description: description, callID: callID, toolName: toolName, toolArgs: toolArgs)
                    }
                }
            }
        } catch {
            fputs("ERROR: JSON parse error: \(error)\n", stderr)
        }
    }

    func extractEventType(from json: [String: Any]) -> String {

        if json["Tools"] != nil {
            return "Parallel Tools Start"
        }

        if json["Content"] != nil {
            return "Chat Chunk"
        }

        if json["Message"] != nil && json["IsActive"] != nil {
            return "Optimization"
        }

        if json["ToolCalls"] != nil {
            return "Chat Complete"
        }

        if json["ToolCallID"] != nil && json["ToolName"] != nil && json["Status"] != nil {
            return "Tool Execution Progress"
        }

        if json["ToolName"] != nil && json["Arguments"] != nil {
            return "Tool Execution"
        }

        if json["ToolCall"] != nil {
            return "Tool Approval"
        }

        if json["Reason"] != nil {
            return "Cancelled"
        }

        if json["Model"] != nil {
            return "Chat Start"
        }

        if json["RequestID"] != nil && json["Timestamp"] != nil &&
           json["Content"] == nil && json["ToolCall"] == nil && json["Model"] == nil && json["Message"] == nil {
            return "Approval Cleared"
        }

        if json["RequestID"] != nil {
            return "Event"
        }
        return "Unknown"
    }

    func extractDescription(from json: [String: Any]) -> String {
        // For Content field (ChatChunkEvent), preserve ALL whitespace including spaces and newlines
        if let content = json["Content"] as? String {
            return content  // Don't trim - spaces and newlines are important!
        }

        if let reason = json["Reason"] as? String {
            return "Interrupted: \(reason)"
        }

        if let message = json["Message"] as? String {
            return message
        }

        if let model = json["Model"] as? String {
            return "Model: \(model)"
        }

        if let toolCall = json["ToolCall"] as? [String: Any],
           let function = toolCall["function"] as? [String: Any],
           let toolName = function["name"] as? String {
            return "Tool approval: \(toolName)"
        }

        if let status = json["Status"] as? String {
            return status
        }

        if let error = json["Error"] as? String {
            return "Error: \(error)"
        }

        if let toolName = json["ToolName"] as? String {
            if let status = json["Status"] as? String {
                return "\(toolName): \(status)"
            }
            return "Tool: \(toolName)"
        }

        if json["RequestID"] != nil {
            return "Event received"
        }
        return "No description"
    }
}

// MARK: - Main

signal(SIGTERM, SIG_IGN)
let sigTermSource = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
sigTermSource.setEventHandler {
    exit(0)
}
sigTermSource.resume()

NSApplication.shared.setActivationPolicy(.accessory)
NSApplication.shared.activate(ignoringOtherApps: true)

let window = AgentProgressWindow()

let reader = EventReader(window: window)
reader.startReading()

NSApplication.shared.run()
`, position, alwaysOnTop)
}
