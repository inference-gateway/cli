import Cocoa
import Foundation

// MARK: - Models

struct PauseResumeRequest: Codable {
    let action: String
    let request_id: String
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

// MARK: - Image Enlarged Window

class ImageEnlargedWindow: NSPanel {
    var onClose: (() -> Void)?

    init(image: NSImage, metadata: String) {
        let screen = NSScreen.main!
        let screenFrame = screen.visibleFrame

        let maxWidth = screenFrame.width * 0.8
        let maxHeight = screenFrame.height * 0.8
        let aspectRatio = image.size.width / image.size.height

        var windowSize = image.size
        if windowSize.width > maxWidth {
            windowSize.width = maxWidth
            windowSize.height = maxWidth / aspectRatio
        }
        if windowSize.height > maxHeight {
            windowSize.height = maxHeight
            windowSize.width = maxHeight * aspectRatio
        }

        let frame = NSRect(
            x: screenFrame.midX - windowSize.width / 2,
            y: screenFrame.midY - windowSize.height / 2,
            width: windowSize.width,
            height: windowSize.height
        )

        super.init(
            contentRect: frame,
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )

        self.title = metadata
        self.level = .modalPanel
        self.isMovableByWindowBackground = true

        let imageView = NSImageView(frame: NSRect(origin: .zero, size: windowSize))
        imageView.image = image
        imageView.imageScaling = .scaleProportionallyUpOrDown
        self.contentView = imageView

        self.makeKeyAndOrderFront(nil)

        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            if event.keyCode == 53 {
                self?.close()
                return nil
            }
            return event
        }
    }

    override func close() {
        onClose?()
        super.close()
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

        let minY = screenFrame.minY
        let maxY = screenFrame.maxY - window.frame.height
        newOrigin.y = max(minY, min(maxY, newOrigin.y))

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

// MARK: - Flipped View for Stack Container

class FlippedView: NSView {
    override var isFlipped: Bool {
        return true
    }
}

// MARK: - Clickable Image View

class ClickableImageView: NSView {
    private let imageView: NSImageView
    private let fullImage: NSImage
    private let metadata: String
    private var enlargedWindow: ImageEnlargedWindow?

    init(thumbnail: NSImage, fullImage: NSImage, metadata: String) {
        self.fullImage = fullImage
        self.metadata = metadata
        self.imageView = NSImageView(image: thumbnail)

        super.init(frame: NSRect(x: 0, y: 0, width: thumbnail.size.width, height: thumbnail.size.height))

        self.addSubview(imageView)
        imageView.frame = self.bounds
        imageView.imageScaling = .scaleProportionallyUpOrDown

        self.wantsLayer = true
        self.layer?.borderColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 0.5).cgColor
        self.layer?.borderWidth = 2
        self.layer?.cornerRadius = 8
        self.layer?.masksToBounds = true

        addTrackingArea(NSTrackingArea(
            rect: bounds,
            options: [.mouseEnteredAndExited, .activeInKeyWindow],
            owner: self,
            userInfo: nil
        ))
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) not implemented")
    }

    override func mouseDown(with event: NSEvent) {
        showEnlarged()
    }

    override func mouseEntered(with event: NSEvent) {
        NSCursor.pointingHand.set()
        self.layer?.borderColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0).cgColor
    }

    override func mouseExited(with event: NSEvent) {
        NSCursor.arrow.set()
        self.layer?.borderColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 0.5).cgColor
    }

    private func showEnlarged() {
        if enlargedWindow == nil {
            enlargedWindow = ImageEnlargedWindow(image: fullImage, metadata: metadata)
            enlargedWindow?.onClose = { [weak self] in
                self?.enlargedWindow = nil
            }
        }
    }
}

// MARK: - Main Floating Window

class FloatingWindow: NSPanel {
    let scrollView = NSScrollView()
    let contentStack = NSStackView()
    let textView = NSTextView()
    let controlBox = NSView()
    let pauseButton = NSButton(title: "⏸ Pause", target: nil, action: nil)
    let resumeButton = NSButton(title: "▶ Resume", target: nil, action: nil)

    var isPaused = false
    var currentRequestID: String?
    var isMinimized = false
    var fullFrame: NSRect?
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

        super.init(contentRect: frame, styleMask: [.titled, .resizable, .miniaturizable], backing: .buffered, defer: false)

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

        self.titlebarAppearsTransparent = false
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
            self.controlBox.isHidden = true
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
        self.titlebarAppearsTransparent = false
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
        self.controlBox.isHidden = false

        NSAnimationContext.runAnimationGroup({ context in
            context.duration = 0.3
            context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            self.animator().setFrame(savedFrame, display: true)
            self.animator().alphaValue = 0.95
        }, completionHandler: {
            self.updateTextContainerWidth()
            self.updateScrollViewInsets()
        })
    }

    func updateTextContainerWidth() {
        guard !isMinimized else { return }

        DispatchQueue.main.async {
            let visibleWidth = self.scrollView.contentView.bounds.width
            let textInset: CGFloat = 16
            let availableWidth = visibleWidth - (textInset * 2)

            for view in self.contentStack.arrangedSubviews {
                if let textField = view as? NSTextField {
                    textField.preferredMaxLayoutWidth = availableWidth
                }
            }
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

        textView.frame = NSRect(x: 0, y: 0, width: contentView.bounds.width, height: 100)
        textView.autoresizingMask = [.width]
        textView.isEditable = false
        textView.isSelectable = true
        textView.backgroundColor = NSColor(red: 0.10, green: 0.11, blue: 0.15, alpha: 1.0)
        textView.textColor = NSColor(red: 0.66, green: 0.69, blue: 0.84, alpha: 1.0)
        textView.font = NSFont.monospacedSystemFont(ofSize: 13, weight: .regular)
        textView.textContainerInset = NSSize(width: 16, height: 16)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.textContainer?.widthTracksTextView = true
        textView.textContainer?.lineBreakMode = .byWordWrapping
        textView.textContainer?.containerSize = NSSize(width: contentView.bounds.width, height: CGFloat.greatestFiniteMagnitude)

        contentStack.orientation = .vertical
        contentStack.alignment = .leading
        contentStack.spacing = 8
        contentStack.edgeInsets = NSEdgeInsets(top: 16, left: 0, bottom: 16, right: 0)

        contentStack.frame = NSRect(x: 0, y: 0, width: contentView.bounds.width, height: contentView.bounds.height)
        contentStack.autoresizingMask = [.width]

        contentStack.addArrangedSubview(textView)

        let containerView = FlippedView(frame: NSRect(x: 0, y: 0, width: contentView.bounds.width, height: contentView.bounds.height))
        containerView.autoresizingMask = [.width]
        contentStack.frame.origin = NSPoint(x: 0, y: 0)
        containerView.addSubview(contentStack)

        scrollView.documentView = containerView
        scrollView.drawsBackground = false
        scrollView.backgroundColor = .clear
        scrollView.hasVerticalScroller = true
        scrollView.hasHorizontalScroller = false
        scrollView.autohidesScrollers = true
        scrollView.frame = contentView.bounds
        scrollView.autoresizingMask = [.width, .height]

        scrollView.automaticallyAdjustsContentInsets = false
        scrollView.contentInsets = NSEdgeInsets(top: 0, left: 0, bottom: 70, right: 0)

        contentView.addSubview(scrollView)

        controlBox.wantsLayer = true
        controlBox.layer?.backgroundColor = NSColor(red: 0.14, green: 0.16, blue: 0.23, alpha: 1.0).cgColor
        controlBox.layer?.borderColor = NSColor(red: 0.48, green: 0.64, blue: 0.97, alpha: 1.0).cgColor
        controlBox.layer?.borderWidth = 2
        controlBox.layer?.cornerRadius = 8
        controlBox.frame = NSRect(x: 10, y: 10, width: contentView.bounds.width - 20, height: 50)
        controlBox.autoresizingMask = [.width, .maxYMargin]
        controlBox.isHidden = false

        pauseButton.bezelStyle = .regularSquare
        pauseButton.target = self
        pauseButton.action = #selector(pauseClicked)
        pauseButton.frame = NSRect(x: 10, y: 10, width: 160, height: 30)
        pauseButton.contentTintColor = NSColor(red: 0.97, green: 0.64, blue: 0.30, alpha: 1.0)
        pauseButton.wantsLayer = true
        pauseButton.layer?.backgroundColor = NSColor(red: 0.97, green: 0.64, blue: 0.30, alpha: 0.2).cgColor
        pauseButton.layer?.cornerRadius = 6
        pauseButton.isHidden = false

        resumeButton.bezelStyle = .regularSquare
        resumeButton.target = self
        resumeButton.action = #selector(resumeClicked)
        resumeButton.frame = NSRect(x: 10, y: 10, width: 160, height: 30)
        resumeButton.contentTintColor = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 1.0)
        resumeButton.wantsLayer = true
        resumeButton.layer?.backgroundColor = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 0.2).cgColor
        resumeButton.layer?.cornerRadius = 6
        resumeButton.isHidden = true

        controlBox.addSubview(pauseButton)
        controlBox.addSubview(resumeButton)

        contentView.addSubview(controlBox)

        NotificationCenter.default.addObserver(
            forName: NSWindow.didResizeNotification,
            object: self,
            queue: .main
        ) { [weak self] _ in
            self?.updateScrollViewInsets()
            self?.updateTextContainerWidth()
        }

        updateScrollViewInsets()
        updateTextContainerWidth()
    }

    @objc func pauseClicked() {
        guard let requestID = currentRequestID else { return }
        isPaused = true

        let request = PauseResumeRequest(action: "pause", request_id: requestID)
        if let jsonData = try? JSONEncoder().encode(request),
           let jsonString = String(data: jsonData, encoding: .utf8) {
            print(jsonString)
            fflush(stdout)
        }

        pauseButton.isHidden = true
        resumeButton.isHidden = false

        let orange = NSColor(red: 0.97, green: 0.64, blue: 0.30, alpha: 1.0)
        appendText("\n⏸ Execution paused by user\n", color: orange)
    }

    @objc func resumeClicked() {
        guard let requestID = currentRequestID else { return }
        isPaused = false

        let request = PauseResumeRequest(action: "resume", request_id: requestID)
        if let jsonData = try? JSONEncoder().encode(request),
           let jsonString = String(data: jsonData, encoding: .utf8) {
            print(jsonString)
            fflush(stdout)
        }

        pauseButton.isHidden = false
        resumeButton.isHidden = true

        let green = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 1.0)
        appendText("\n▶ Execution resuming...\n", color: green)
    }

    func appendText(_ text: String, color: NSColor? = nil) {
        DispatchQueue.main.async {
            let attrs: [NSAttributedString.Key: Any] = [
                .foregroundColor: color ?? self.textView.textColor!,
                .font: self.textView.font!
            ]
            let attrString = NSAttributedString(string: text, attributes: attrs)
            self.textView.textStorage?.append(attrString)

            self.textView.layoutManager?.ensureLayout(for: self.textView.textContainer!)

            if let layoutManager = self.textView.layoutManager,
               let textContainer = self.textView.textContainer {
                layoutManager.ensureLayout(for: textContainer)
                let usedRect = layoutManager.usedRect(for: textContainer)
                let inset = self.textView.textContainerInset
                let newHeight = usedRect.height + inset.height * 2

                var textFrame = self.textView.frame
                textFrame.size.height = newHeight
                self.textView.frame = textFrame
            }

            self.updateStackViewHeight()
            self.scrollToBottomIfNeeded()
        }
    }

    func appendImage(_ imageData: String, mimeType: String, width: Int, height: Int, toolName: String) {
        DispatchQueue.main.async {
            guard let data = Data(base64Encoded: imageData),
                  let fullImage = NSImage(data: data) else {
                return
            }

            let thumbnailSize = NSSize(width: 200, height: 150)
            let thumbnail = self.resizeImage(fullImage, to: thumbnailSize)

            let imageContainer = ClickableImageView(
                thumbnail: thumbnail,
                fullImage: fullImage,
                metadata: "\(toolName) - \(width)x\(height)"
            )

            self.contentStack.addArrangedSubview(imageContainer)
            self.updateStackViewHeight()
            self.scrollToBottomIfNeeded()
        }
    }

    func updateStackViewHeight() {
        let fittingSize = contentStack.fittingSize
        let minHeight = scrollView.contentView.bounds.height
        let newHeight = max(fittingSize.height, minHeight)

        var stackFrame = contentStack.frame
        stackFrame.size.height = newHeight
        stackFrame.size.width = scrollView.contentView.bounds.width
        contentStack.frame = stackFrame

        if let containerView = scrollView.documentView {
            var containerFrame = containerView.frame
            containerFrame.size.height = newHeight
            containerFrame.size.width = scrollView.contentView.bounds.width
            containerView.frame = containerFrame

            scrollView.needsLayout = true
            scrollView.layoutSubtreeIfNeeded()
        }
    }

    func isScrolledNearBottom() -> Bool {
        guard let documentView = scrollView.documentView else { return true }

        let visibleRect = scrollView.contentView.documentVisibleRect
        let documentHeight = documentView.bounds.height
        let bottomY = visibleRect.origin.y + visibleRect.height

        let threshold: CGFloat = 100
        return (documentHeight - bottomY) < threshold
    }

    func scrollToBottomIfNeeded() {
        guard isScrolledNearBottom() else { return }

        if let documentView = scrollView.documentView {
            let newScrollOrigin = NSPoint(x: 0, y: max(0, documentView.bounds.height - scrollView.contentView.bounds.height))
            scrollView.contentView.scroll(to: newScrollOrigin)
            scrollView.reflectScrolledClipView(scrollView.contentView)
        }
    }

    func resizeImage(_ image: NSImage, to targetSize: NSSize) -> NSImage {
        let aspectRatio = image.size.width / image.size.height
        var newSize = targetSize

        if aspectRatio > targetSize.width / targetSize.height {
            newSize.height = targetSize.width / aspectRatio
        } else {
            newSize.width = targetSize.height * aspectRatio
        }

        let resized = NSImage(size: newSize)
        resized.lockFocus()
        image.draw(in: NSRect(origin: .zero, size: newSize))
        resized.unlockFocus()
        return resized
    }

    func extractImagesFromJSON(_ json: [String: Any]) -> [[String: Any]]? {
        if let images = json["Images"] as? [[String: Any]] {
            return images
        }

        guard let nsArray = json["Images"] as? NSArray else {
            return nil
        }

        let converted = nsArray.compactMap { element -> [String: Any]? in
            if let nsDict = element as? NSDictionary {
                return nsDict as? [String: Any]
            }
            return element as? [String: Any]
        }

        return converted.isEmpty ? nil : converted
    }

    func setRequestID(requestID: String) {
        DispatchQueue.main.async {
            self.currentRequestID = requestID
        }
    }

    func updatePauseState(paused: Bool) {
        DispatchQueue.main.async {
            self.isPaused = paused
            self.pauseButton.isHidden = paused
            self.resumeButton.isHidden = !paused
        }
    }

    func updateScrollViewInsets() {
        updateStackViewHeight()
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

        }
    }

    func hideBorderOverlay() {
        DispatchQueue.main.async {
            for window in self.borderWindows {
                window.close()
            }
            self.borderWindows.removeAll()
        }
    }

    // MARK: - Click Indicator Control

    func showClickIndicator(x: CGFloat, y: CGFloat) {
        DispatchQueue.main.async {
            _ = ClickIndicatorWindow(x: x, y: y)
        }
    }

    func showMoveIndicator(fromX: CGFloat, fromY: CGFloat, toX: CGFloat, toY: CGFloat) {
        DispatchQueue.main.async {
            _ = MoveTrailWindow(fromX: fromX, fromY: fromY, toX: toX, toY: toY)
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

                if let requestID = json["RequestID"] as? String {
                    window.setRequestID(requestID: requestID)
                }

            case "Chat Chunk":
                window.appendText(description)

            case "Computer Use Paused":
                window.updatePauseState(paused: true)

            case "Computer Use Resumed":
                window.updatePauseState(paused: false)

            case "Tool Approval Notification":
                if let message = json["Message"] as? String {
                    let yellow = NSColor(red: 0.97, green: 0.85, blue: 0.30, alpha: 1.0)
                    window.appendText("\n\(message)\n", color: yellow)
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
                guard let toolName = json["ToolName"] as? String,
                      let status = json["Status"] as? String else { break }

                if status == "completed" {
                    if let images = window.extractImagesFromJSON(json), !images.isEmpty {
                        window.appendText("\n")

                        for imageData in images {
                            guard let base64 = imageData["data"] as? String,
                                  let mimeType = imageData["mime_type"] as? String,
                                  let displayName = imageData["display_name"] as? String else {
                                continue
                            }

                            window.appendImage(
                                base64,
                                mimeType: mimeType,
                                width: 1920,
                                height: 1080,
                                toolName: displayName
                            )
                        }

                        window.appendText("\n")
                    } else {
                        let green = NSColor(red: 0.45, green: 0.87, blue: 0.68, alpha: 1.0)
                        window.appendText("✓ \(toolName) completed\n", color: green)
                    }
                } else if status == "failed" {
                    let red = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 1.0)
                    window.appendText("✗ \(toolName) failed\n", color: red)
                }

            case "Tool Failed", "Tool Rejected":
                let red = NSColor(red: 0.97, green: 0.46, blue: 0.56, alpha: 1.0)
                window.appendText("\n✗ \(description)\n", color: red)

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
        if json["Tools"] != nil { return "Parallel Tools Start" }
        if json["ToolName"] != nil && json["Status"] != nil { return "Tool Execution Progress" }
        if let typeName = json["$type"] as? String {
            if typeName.contains("ComputerUsePaused") { return "Computer Use Paused" }
            if typeName.contains("ComputerUseResumed") { return "Computer Use Resumed" }
        }
        if json["Message"] != nil && json["ToolName"] != nil && json["Timestamp"] != nil {
            return "Tool Approval Notification"
        }
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
