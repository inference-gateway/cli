import Cocoa
import Foundation

// MARK: - Models

struct ApprovalResponse: Codable {
    let call_id: String
    let action: Int  // 0=Approve, 1=Reject, 2=AutoAccept
}

// MARK: - Border Overlay Windows

class BorderWindow: NSWindow {
    init(frame: NSRect, color: NSColor) {
        super.init(contentRect: frame, styleMask: .borderless, backing: .buffered, defer: false)
        self.backgroundColor = color
        self.isOpaque = false
        self.level = .screenSaver
        self.ignoresMouseEvents = true
        self.collectionBehavior = [.canJoinAllSpaces, .stationary]
        self.orderFront(nil)
    }
}

// MARK: - Click Indicator Window

private var activeClickIndicators: [ClickIndicatorWindow] = []

class ClickIndicatorWindow: NSWindow {
    private var closeTimer: Timer?

    init(x: CGFloat, y: CGFloat) {
        let size: CGFloat = 40
        let frame = NSRect(x: x - size/2, y: y - size/2, width: size, height: size)

        super.init(contentRect: frame, styleMask: .borderless, backing: .buffered, defer: false)

        self.isReleasedWhenClosed = false

        self.backgroundColor = .clear
        self.isOpaque = false
        self.level = .screenSaver
        self.ignoresMouseEvents = true
        self.collectionBehavior = [.canJoinAllSpaces, .stationary]

        let indicatorView = ClickIndicatorView(frame: NSRect(x: 0, y: 0, width: size, height: size))
        self.contentView = indicatorView

        self.orderFront(nil)

        activeClickIndicators.append(self)

        self.alphaValue = 0
        self.alphaValue = 1.0

        self.closeTimer = Timer.scheduledTimer(withTimeInterval: 0.6, repeats: false) { [weak self] timer in
            guard let self = self else { return }
            timer.invalidate()

            if let index = activeClickIndicators.firstIndex(where: { $0 === self }) {
                activeClickIndicators.remove(at: index)
            }

            self.orderOut(nil)
        }
    }

    deinit {
        closeTimer?.invalidate()
    }
}

class ClickIndicatorView: NSView {
    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)

        guard let context = NSGraphicsContext.current?.cgContext else { return }

        let outerColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 0.8)
        context.setStrokeColor(outerColor.cgColor)
        context.setLineWidth(3)
        let outerRect = bounds.insetBy(dx: 2, dy: 2)
        context.strokeEllipse(in: outerRect)

        let innerColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 0.3)
        context.setFillColor(innerColor.cgColor)
        let innerRect = bounds.insetBy(dx: 12, dy: 12)
        context.fillEllipse(in: innerRect)
    }
}

// MARK: - Move Trail Indicator Window

private var activeMoveIndicators: [MoveTrailWindow] = []

class MoveTrailWindow: NSWindow {
    init(fromX: CGFloat, fromY: CGFloat, toX: CGFloat, toY: CGFloat) {
        let screen = NSScreen.main!
        let screenFrame = screen.frame

        let fromPoint = NSPoint(x: fromX, y: screenFrame.height - fromY)
        let toPoint = NSPoint(x: toX, y: screenFrame.height - toY)

        super.init(
            contentRect: screenFrame,
            styleMask: .borderless,
            backing: .buffered,
            defer: false
        )

        self.isOpaque = false
        self.backgroundColor = .clear
        self.level = .screenSaver
        self.ignoresMouseEvents = true
        self.collectionBehavior = [.canJoinAllSpaces, .stationary]

        let trailView = MoveTrailView(from: fromPoint, to: toPoint)
        self.contentView = trailView

        self.makeKeyAndOrderFront(nil)

        activeMoveIndicators.append(self)

        Timer.scheduledTimer(withTimeInterval: 0.8, repeats: false) { [weak self] timer in
            timer.invalidate()
            guard let self = self else { return }

            if let index = activeMoveIndicators.firstIndex(where: { $0 === self }) {
                activeMoveIndicators.remove(at: index)
            }

            self.orderOut(nil)
        }
    }
}

class MoveTrailView: NSView {
    let fromPoint: NSPoint
    let toPoint: NSPoint
    var animationProgress: CGFloat = 0.0
    var displayLink: CVDisplayLink?

    init(from: NSPoint, to: NSPoint) {
        self.fromPoint = from
        self.toPoint = to
        super.init(frame: .zero)

        animateTrail()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) not implemented")
    }

    func animateTrail() {
        Timer.scheduledTimer(withTimeInterval: 0.016, repeats: true) { [weak self] timer in
            guard let self = self else {
                timer.invalidate()
                return
            }

            self.animationProgress += 0.03
            if self.animationProgress >= 1.0 {
                self.animationProgress = 1.0
                timer.invalidate()
            }
            self.needsDisplay = true
        }
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)

        guard let context = NSGraphicsContext.current?.cgContext else { return }

        let path = NSBezierPath()
        path.move(to: fromPoint)

        let currentX = fromPoint.x + (toPoint.x - fromPoint.x) * animationProgress
        let currentY = fromPoint.y + (toPoint.y - fromPoint.y) * animationProgress

        path.line(to: NSPoint(x: currentX, y: currentY))

        let trailColor = NSColor(red: 0.3, green: 0.9, blue: 0.7, alpha: 1.0 - animationProgress * 0.5)
        trailColor.setStroke()
        path.lineWidth = 3.0
        path.stroke()

        if animationProgress > 0.1 {
            let arrowSize: CGFloat = 10.0

            let dx = toPoint.x - fromPoint.x
            let dy = toPoint.y - fromPoint.y
            let angle = atan2(dy, dx)

            context.saveGState()
            context.translateBy(x: currentX, y: currentY)
            context.rotate(by: angle)

            let arrowPath = NSBezierPath()
            arrowPath.move(to: NSPoint(x: 0, y: 0))
            arrowPath.line(to: NSPoint(x: -arrowSize, y: arrowSize/2))
            arrowPath.line(to: NSPoint(x: -arrowSize, y: -arrowSize/2))
            arrowPath.close()

            trailColor.setFill()
            arrowPath.fill()

            context.restoreGState()
        }
    }
}

// MARK: - Clickable View for Minimized State

class ClickableView: NSView {
    var onClicked: (() -> Void)?
    var dragStartLocation: NSPoint?
    var hasDragged = false

    override func mouseDown(with event: NSEvent) {
        dragStartLocation = event.locationInWindow
        hasDragged = false
    }

    override func mouseDragged(with event: NSEvent) {
        guard let window = self.window,
              let startLocation = dragStartLocation else { return }

        let currentLocation = event.locationInWindow
        let deltaY = currentLocation.y - startLocation.y

        if abs(deltaY) > 3 {
            hasDragged = true
        }

        guard let screen = NSScreen.main else { return }
        let screenFrame = screen.visibleFrame

        var newOrigin = window.frame.origin
        newOrigin.y += deltaY
        newOrigin.x = screenFrame.maxX - window.frame.width

        window.setFrameOrigin(newOrigin)
    }

    override func mouseUp(with event: NSEvent) {
        if !hasDragged {
            onClicked?()
        }

        dragStartLocation = nil
        hasDragged = false
    }
}

// MARK: - Main Floating Window

class FloatingWindow: NSPanel {
    let scrollView = NSScrollView()
    let textView = NSTextView()
    let approvalBox = NSView()
    let approveButton = NSButton(title: "✓ Approve", target: nil, action: nil)
    let rejectButton = NSButton(title: "✗ Reject", target: nil, action: nil)
    let autoButton = NSButton(title: "Auto-Approve", target: nil, action: nil)

    var currentCallID: String?
    var isMinimized = false
    var fullFrame: NSRect?
    var wasApprovalVisible = false
    let minimizedWidth: CGFloat = 40
    let minimizedHeight: CGFloat = 150
    var minimizedYPosition: CGFloat?

    var borderWindows: [BorderWindow] = []

    init(position: String, alwaysOnTop: Bool) {
        let screenFrame = NSScreen.main!.visibleFrame
        let windowWidth: CGFloat = 450
        let windowHeight: CGFloat = 350

        var xPos: CGFloat
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

        super.init(contentRect: frame, styleMask: [.titled, .resizable, .miniaturizable, .fullSizeContentView], backing: .buffered, defer: false)

        self.title = "Computer Use"
        self.isFloatingPanel = true
        self.level = alwaysOnTop ? .floating : .normal
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

    deinit {
        NotificationCenter.default.removeObserver(self, name: NSWindow.didResizeNotification, object: nil)
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
        wasApprovalVisible = !self.approvalBox.isHidden

        let screenFrame = screen.visibleFrame
        let xPos = screenFrame.maxX - minimizedWidth

        let yPos: CGFloat
        if let savedY = minimizedYPosition {
            yPos = savedY
        } else {
            yPos = screenFrame.midY - (minimizedHeight / 2)
        }

        let minimizedFrame = NSRect(x: xPos, y: yPos, width: minimizedWidth, height: minimizedHeight)

        NSAnimationContext.runAnimationGroup({ context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            self.animator().setFrame(minimizedFrame, display: true)
            self.animator().alphaValue = 1.0
        }, completionHandler: {
            self.scrollView.isHidden = true
            self.approvalBox.isHidden = true
            self.titleVisibility = .hidden
            self.titlebarAppearsTransparent = true
            self.standardWindowButton(.closeButton)?.alphaValue = 0
            self.standardWindowButton(.miniaturizeButton)?.alphaValue = 0
            self.standardWindowButton(.zoomButton)?.alphaValue = 0
            self.isOpaque = true
            self.backgroundColor = NSColor(calibratedWhite: 0.1, alpha: 1.0)

            self.isMovableByWindowBackground = false
            self.styleMask.remove(.resizable)

            self.updateMinimizedUI()
        })
    }

    func restoreWindow() {
        guard let savedFrame = fullFrame else { return }

        minimizedYPosition = self.frame.origin.y

        isMinimized = false

        self.titleVisibility = .visible
        self.titlebarAppearsTransparent = true
        self.standardWindowButton(.closeButton)?.alphaValue = 0
        self.standardWindowButton(.miniaturizeButton)?.alphaValue = 1.0
        self.standardWindowButton(.zoomButton)?.alphaValue = 0
        self.isOpaque = false
        self.backgroundColor = NSColor(calibratedWhite: 0.1, alpha: 0.85)

        self.isMovableByWindowBackground = true
        self.styleMask.insert(.resizable)

        if let contentView = self.contentView {
            contentView.subviews.forEach { view in
                if view.identifier?.rawValue == "minimizedLabel" {
                    view.removeFromSuperview()
                }
            }
        }

        self.scrollView.isHidden = false
        self.approvalBox.isHidden = !wasApprovalVisible

        NSAnimationContext.runAnimationGroup({ context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            self.animator().setFrame(savedFrame, display: true)
            self.animator().alphaValue = 0.95
        }, completionHandler: {
            self.updateTextContainerWidth()
        })
    }

    func updateTextContainerWidth() {
        guard !isMinimized else { return }

        DispatchQueue.main.async {
            let visibleWidth = self.scrollView.contentView.bounds.width

            var newFrame = self.textView.frame
            newFrame.size.width = visibleWidth
            self.textView.frame = newFrame

            let textInset: CGFloat = 16
            let availableWidth = visibleWidth - (textInset * 2)

            self.textView.textContainer?.containerSize = NSSize(
                width: availableWidth,
                height: CGFloat.greatestFiniteMagnitude
            )

            self.textView.layoutManager?.ensureLayout(for: self.textView.textContainer!)
            self.textView.setNeedsDisplay(self.textView.bounds)
        }
    }

    func updateMinimizedUI() {
        guard let contentView = self.contentView else { return }

        contentView.subviews.forEach { view in
            if view.identifier?.rawValue == "minimizedLabel" {
                view.removeFromSuperview()
            }
        }

        let clickableView = ClickableView(frame: NSRect(x: 0, y: 0, width: minimizedWidth, height: minimizedHeight))
        clickableView.identifier = NSUserInterfaceItemIdentifier("minimizedLabel")
        clickableView.onClicked = { [weak self] in
            self?.restoreWindow()
        }
        contentView.addSubview(clickableView)

        let labelHeight: CGFloat = 30
        let labelY = (minimizedHeight - labelHeight) / 2
        let label = NSTextField(labelWithString: "●")
        label.frame = NSRect(x: 0, y: labelY, width: minimizedWidth, height: labelHeight)
        label.alignment = .center
        label.font = NSFont.systemFont(ofSize: 20)
        label.textColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0)
        label.backgroundColor = .clear
        label.isBordered = false
        label.isEditable = false
        label.isSelectable = false
        clickableView.addSubview(label)
    }

    override func mouseDown(with event: NSEvent) {
        if !isMinimized {
            super.mouseDown(with: event)
        }
    }

    func setupUI() {
        guard let contentView = self.contentView else { return }

        let titleBarHeight = self.frame.height - contentView.frame.height
        let topPadding = titleBarHeight > 0 ? titleBarHeight : 28

        textView.frame = contentView.bounds
        textView.autoresizingMask = [.width]
        textView.isEditable = false
        textView.isSelectable = true
        textView.backgroundColor = NSColor(red: 0.10, green: 0.11, blue: 0.15, alpha: 1.0)
        textView.textColor = NSColor(red: 0.66, green: 0.69, blue: 0.84, alpha: 1.0)
        textView.font = NSFont.systemFont(ofSize: 13)
        textView.textContainerInset = NSSize(width: 16, height: 16)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.textContainer?.widthTracksTextView = true
        textView.textContainer?.containerSize = NSSize(width: contentView.bounds.width, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.lineBreakMode = .byWordWrapping

        var scrollFrame = contentView.bounds
        scrollFrame.origin.y = 0
        scrollFrame.size.height = contentView.bounds.height - topPadding

        scrollView.documentView = textView
        scrollView.hasVerticalScroller = true
        scrollView.hasHorizontalScroller = false
        scrollView.autohidesScrollers = true
        scrollView.frame = scrollFrame
        scrollView.autoresizingMask = [.width, .height]
        contentView.addSubview(scrollView)

        approvalBox.wantsLayer = true
        approvalBox.layer?.backgroundColor = NSColor(red: 0.14, green: 0.16, blue: 0.23, alpha: 1.0).cgColor
        approvalBox.layer?.borderColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0).cgColor
        approvalBox.layer?.borderWidth = 2
        approvalBox.layer?.cornerRadius = 8
        approvalBox.frame = NSRect(x: 10, y: 10, width: contentView.bounds.width - 20, height: 50)
        approvalBox.autoresizingMask = [.width, .maxYMargin]
        approvalBox.isHidden = true

        approveButton.bezelStyle = .regularSquare
        approveButton.target = self
        approveButton.action = #selector(approveClicked)
        approveButton.frame = NSRect(x: 10, y: 10, width: 120, height: 30)
        approveButton.contentTintColor = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 1.0)
        approveButton.wantsLayer = true
        approveButton.layer?.backgroundColor = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 0.2).cgColor
        approveButton.layer?.cornerRadius = 6

        rejectButton.bezelStyle = .regularSquare
        rejectButton.target = self
        rejectButton.action = #selector(rejectClicked)
        rejectButton.frame = NSRect(x: 140, y: 10, width: 120, height: 30)
        rejectButton.contentTintColor = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 1.0)
        rejectButton.wantsLayer = true
        rejectButton.layer?.backgroundColor = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 0.2).cgColor
        rejectButton.layer?.cornerRadius = 6

        autoButton.bezelStyle = .regularSquare
        autoButton.target = self
        autoButton.action = #selector(autoClicked)
        autoButton.frame = NSRect(x: 270, y: 10, width: 140, height: 30)
        autoButton.contentTintColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0)
        autoButton.wantsLayer = true
        autoButton.layer?.backgroundColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 0.2).cgColor
        autoButton.layer?.cornerRadius = 6

        approvalBox.addSubview(approveButton)
        approvalBox.addSubview(rejectButton)
        approvalBox.addSubview(autoButton)

        contentView.addSubview(approvalBox)

        NotificationCenter.default.addObserver(
            forName: NSWindow.didResizeNotification,
            object: self,
            queue: .main
        ) { [weak self] _ in
            self?.updateTextContainerWidth()
        }

        updateTextContainerWidth()

        fputs("UI ready for output\n", stderr)
        fflush(stderr)
    }

    @objc func approveClicked() {
        sendApproval(action: 0)
    }

    @objc func rejectClicked() {
        sendApproval(action: 1)
    }

    @objc func autoClicked() {
        sendApproval(action: 2)
    }

    func sendApproval(action: Int) {
        guard let callID = currentCallID else { return }
        let response = ApprovalResponse(call_id: callID, action: action)
        if let jsonData = try? JSONEncoder().encode(response),
           let jsonString = String(data: jsonData, encoding: .utf8) {
            print(jsonString)
            fflush(stdout)
        }
        approvalBox.isHidden = true
        currentCallID = nil
    }

    func appendText(_ text: String, color: NSColor? = nil) {
        DispatchQueue.main.async {
            let attrs: [NSAttributedString.Key: Any] = [
                .foregroundColor: color ?? self.textView.textColor!,
                .font: self.textView.font!
            ]
            let attrString = NSAttributedString(string: text, attributes: attrs)
            self.textView.textStorage?.append(attrString)
            self.textView.scrollToEndOfDocument(nil)
        }
    }

    func showApproval(callID: String, toolName: String) {
        DispatchQueue.main.async {
            self.currentCallID = callID
            self.approvalBox.isHidden = false
        }
    }

    // MARK: - Border Overlay Control

    func showBorderOverlay() {
        DispatchQueue.main.async {
            guard self.borderWindows.isEmpty else { return }
            guard let screen = NSScreen.main else { return }

            let frame = screen.visibleFrame
            let borderWidth: CGFloat = 3
            let borderColor = NSColor(red: 0.3, green: 0.6, blue: 1.0, alpha: 0.95)

            self.borderWindows.append(BorderWindow(
                frame: NSRect(x: frame.minX, y: frame.maxY - borderWidth, width: frame.width, height: borderWidth),
                color: borderColor
            ))

            self.borderWindows.append(BorderWindow(
                frame: NSRect(x: frame.minX, y: frame.minY, width: frame.width, height: borderWidth),
                color: borderColor
            ))

            self.borderWindows.append(BorderWindow(
                frame: NSRect(x: frame.minX, y: frame.minY, width: borderWidth, height: frame.height),
                color: borderColor
            ))

            self.borderWindows.append(BorderWindow(
                frame: NSRect(x: frame.maxX - borderWidth, y: frame.minY, width: borderWidth, height: frame.height),
                color: borderColor
            ))

            fputs("Border overlay shown\n", stderr)
            fflush(stderr)
        }
    }

    func hideBorderOverlay() {
        DispatchQueue.main.async {
            for window in self.borderWindows {
                window.close()
            }
            self.borderWindows.removeAll()
            fputs("Border overlay hidden\n", stderr)
            fflush(stderr)
        }
    }

    // MARK: - Click Indicator Control

    func showClickIndicator(x: CGFloat, y: CGFloat) {
        DispatchQueue.main.async {
            _ = ClickIndicatorWindow(x: x, y: y)
            fputs("Click indicator shown at (\(x), \(y))\n", stderr)
            fflush(stderr)
        }
    }

    func showMoveIndicator(fromX: CGFloat, fromY: CGFloat, toX: CGFloat, toY: CGFloat) {
        DispatchQueue.main.async {
            _ = MoveTrailWindow(fromX: fromX, fromY: fromY, toX: toX, toY: toY)
            fputs("Move trail shown from (\(fromX), \(fromY)) to (\(toX), \(toY))\n", stderr)
            fflush(stderr)
        }
    }
}

// MARK: - Event Reader

class EventReader {
    let window: FloatingWindow

    init(window: FloatingWindow) {
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
            guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                return
            }

            let eventType = extractEventType(from: json)
            let description = extractDescription(from: json)

            switch eventType {
            case "Chat Start":
                let blue = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0)
                window.appendText("\n● Starting...\n\n", color: blue)

            case "Chat Chunk":
                window.appendText(description)

            case "Tool Approval":
                if let toolCall = json["ToolCall"] as? [String: Any],
                   let callID = toolCall["id"] as? String,
                   let function = toolCall["function"] as? [String: Any],
                   let toolName = function["name"] as? String {
                    window.showApproval(callID: callID, toolName: toolName)
                }

            case "Parallel Tools Start":
                if let tools = json["Tools"] as? [[String: Any]] {
                    let blue = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0)
                    for tool in tools {
                        if let toolName = tool["Name"] as? String {
                            let args = tool["Arguments"] as? String ?? ""
                            if !args.isEmpty {
                                window.appendText("\n▶ \(toolName)\n  \(args)\n", color: blue)
                            } else {
                                window.appendText("\n▶ \(toolName)\n", color: blue)
                            }
                        }
                    }
                }

            case "Tool Execution Progress":
                if let toolName = json["ToolName"] as? String,
                   let status = json["Status"] as? String {
                    if status == "completed" {
                        let green = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 1.0)
                        window.appendText("✓ \(toolName) completed\n", color: green)
                    } else if status == "failed" {
                        let red = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 1.0)
                        window.appendText("✗ \(toolName) failed\n", color: red)
                    }
                }

            case "Tool Failed", "Tool Rejected":
                let red = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 1.0)
                window.appendText("\n✗ \(description)\n", color: red)

            case "Approval Cleared":
                DispatchQueue.main.async {
                    self.window.approvalBox.isHidden = true
                }

            case "Border Show":
                window.showBorderOverlay()

            case "Border Hide":
                window.hideBorderOverlay()

            case "Click Indicator":
                if let x = json["X"] as? Double,
                   let y = json["Y"] as? Double {
                    window.showClickIndicator(x: CGFloat(x), y: CGFloat(y))
                }

            case "Move Indicator":
                if let fromX = json["FromX"] as? Int,
                   let fromY = json["FromY"] as? Int,
                   let toX = json["ToX"] as? Int,
                   let toY = json["ToY"] as? Int {
                    window.showMoveIndicator(
                        fromX: CGFloat(fromX),
                        fromY: CGFloat(fromY),
                        toX: CGFloat(toX),
                        toY: CGFloat(toY)
                    )
                }

            default:
                break
            }

        } catch {
            fputs("ERROR: JSON parse error: \(error)\n", stderr)
        }
    }

    func extractEventType(from json: [String: Any]) -> String {
        if json["BorderAction"] as? String == "show" { return "Border Show" }
        if json["BorderAction"] as? String == "hide" { return "Border Hide" }
        if json["FromX"] != nil && json["FromY"] != nil && json["ToX"] != nil && json["ToY"] != nil && json["MoveIndicator"] as? Bool == true { return "Move Indicator" }
        if json["X"] != nil && json["Y"] != nil && json["ClickIndicator"] as? Bool == true { return "Click Indicator" }
        if json["Content"] != nil { return "Chat Chunk" }
        if json["Model"] != nil { return "Chat Start" }
        if json["ToolCall"] != nil { return "Tool Approval" }
        if json["Tools"] != nil { return "Parallel Tools Start" }
        if json["ToolName"] != nil && json["Status"] != nil { return "Tool Execution Progress" }
        if json["RequestID"] != nil && json["Timestamp"] != nil &&
           json["Content"] == nil && json["ToolCall"] == nil { return "Approval Cleared" }
        return "Unknown"
    }

    func extractDescription(from json: [String: Any]) -> String {
        if let content = json["Content"] as? String { return content }
        if let reason = json["Reason"] as? String { return "Interrupted: \(reason)" }
        return ""
    }
}

// MARK: - Main

signal(SIGTERM, SIG_IGN)
let sigTermSource = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
sigTermSource.setEventHandler { exit(0) }
sigTermSource.resume()

NSApplication.shared.setActivationPolicy(.accessory)
NSApplication.shared.activate(ignoringOtherApps: true)

let position = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "top-right"
let alwaysOnTop = CommandLine.arguments.count > 2 ? CommandLine.arguments[2] == "true" : true

let window = FloatingWindow(position: position, alwaysOnTop: alwaysOnTop)
let reader = EventReader(window: window)
reader.startReading()

NSApplication.shared.run()
