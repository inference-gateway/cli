import Cocoa

// MARK: - Window Coordinator
// Manages lifecycle of overlay windows (borders, click indicators, move trails)

class WindowCoordinator {
    // MARK: - Window Collections

    private var borderWindows: [NSWindow] = []
    private var clickIndicators: [NSWindow] = []
    private var moveTrails: [NSWindow] = []

    // MARK: - Thread Safety

    private let queue = DispatchQueue(label: "com.inference-gateway.window-coordinator", qos: .userInteractive)

    // MARK: - Border Overlay

    func showBorder() {
        queue.async { [weak self] in
            guard let self = self else { return }
            guard self.borderWindows.isEmpty else {
                NSLog("[WindowCoordinator] Border already showing")
                return
            }

            DispatchQueue.main.sync {
                guard let screen = NSScreen.main else {
                    NSLog("[WindowCoordinator] No main screen found")
                    return
                }

                let frame = screen.frame
                let width = DesignLayout.borderWidth
                let color = DesignColors.borderHighlight

                NSLog("[WindowCoordinator] Creating border overlay at screen frame: \(frame)")

                self.borderWindows = [
                    self.createBorderWindow(
                        NSRect(x: 0, y: frame.height - width, width: frame.width, height: width),
                        color
                    ),
                    self.createBorderWindow(
                        NSRect(x: 0, y: 0, width: frame.width, height: width),
                        color
                    ),
                    self.createBorderWindow(
                        NSRect(x: 0, y: 0, width: width, height: frame.height),
                        color
                    ),
                    self.createBorderWindow(
                        NSRect(x: frame.width - width, y: 0, width: width, height: frame.height),
                        color
                    )
                ]

                NSLog("[WindowCoordinator] Created \(self.borderWindows.count) border windows")
            }
        }
    }

    func hideBorder() {
        queue.async { [weak self] in
            guard let self = self else { return }
            DispatchQueue.main.sync {
                self.borderWindows.forEach { $0.close() }
                self.borderWindows.removeAll()
            }
        }
    }

    private func createBorderWindow(_ frame: NSRect, _ color: NSColor) -> NSWindow {
        let window = NSWindow(
            contentRect: frame,
            styleMask: .borderless,
            backing: .buffered,
            defer: false
        )
        window.backgroundColor = color
        window.isOpaque = false
        window.level = .screenSaver
        window.ignoresMouseEvents = true
        window.collectionBehavior = [.canJoinAllSpaces, .stationary]
        window.orderFront(nil)
        return window
    }

    // MARK: - Click Indicator

    func showClickIndicator(x: Int, y: Int) {
        queue.async { [weak self] in
            guard let self = self else { return }

            DispatchQueue.main.sync {
                let point = CGPoint(x: CGFloat(x), y: CGFloat(y))

                let indicator = self.createClickIndicatorWindow(at: point)
                self.clickIndicators.append(indicator)

                self.queue.asyncAfter(deadline: .now() + DesignLayout.clickIndicatorDuration) { [weak self] in
                    guard let self = self else { return }
                    DispatchQueue.main.sync {
                        self.clickIndicators.removeAll { $0 === indicator }
                        indicator.close()
                    }
                }
            }
        }
    }

    private func createClickIndicatorWindow(at point: CGPoint) -> NSWindow {
        let size = DesignLayout.clickIndicatorSize
        let frame = NSRect(
            x: point.x - size / 2,
            y: point.y - size / 2,
            width: size,
            height: size
        )

        let window = NSWindow(
            contentRect: frame,
            styleMask: .borderless,
            backing: .buffered,
            defer: false
        )
        window.backgroundColor = .clear
        window.isOpaque = false
        window.level = .screenSaver
        window.ignoresMouseEvents = true

        let indicatorView = ClickIndicatorView(frame: CGRect(x: 0, y: 0, width: size, height: size))
        window.contentView = indicatorView

        window.orderFront(nil)

        window.alphaValue = 0
        NSAnimationContext.runAnimationGroup({ context in
            context.duration = DesignLayout.fastAnimation
            window.animator().alphaValue = 1.0
        })

        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { [weak window] in
            guard let window = window else { return }
            NSAnimationContext.runAnimationGroup({ context in
                context.duration = DesignLayout.fastAnimation
                window.animator().alphaValue = 0.0
            })
        }

        return window
    }

    // MARK: - Move Trail

    func showMoveTrail(from: (Int, Int), to: (Int, Int)) {
        queue.async { [weak self] in
            guard let self = self else { return }

            DispatchQueue.main.sync {
                let fromPoint = CGPoint(
                    x: CGFloat(from.0),
                    y: CGFloat(from.1)
                )
                let toPoint = CGPoint(
                    x: CGFloat(to.0),
                    y: CGFloat(to.1)
                )

                let trail = self.createMoveTrailWindow(from: fromPoint, to: toPoint)
                self.moveTrails.append(trail)

                self.queue.asyncAfter(deadline: .now() + DesignLayout.moveTrailDuration) { [weak self] in
                    guard let self = self else { return }
                    DispatchQueue.main.sync {
                        self.moveTrails.removeAll { $0 === trail }
                        trail.close()
                    }
                }
            }
        }
    }

    private func createMoveTrailWindow(from fromPoint: CGPoint, to toPoint: CGPoint) -> NSWindow {
        let minX = min(fromPoint.x, toPoint.x)
        let minY = min(fromPoint.y, toPoint.y)
        let maxX = max(fromPoint.x, toPoint.x)
        let maxY = max(fromPoint.y, toPoint.y)

        let frame = NSRect(
            x: minX - 10,
            y: minY - 10,
            width: (maxX - minX) + 20,
            height: (maxY - minY) + 20
        )

        let window = NSWindow(
            contentRect: frame,
            styleMask: .borderless,
            backing: .buffered,
            defer: false
        )
        window.backgroundColor = .clear
        window.isOpaque = false
        window.level = .screenSaver
        window.ignoresMouseEvents = true

        let trailView = MoveTrailView(
            frame: CGRect(x: 0, y: 0, width: frame.width, height: frame.height),
            fromPoint: CGPoint(x: fromPoint.x - frame.minX, y: fromPoint.y - frame.minY),
            toPoint: CGPoint(x: toPoint.x - frame.minX, y: toPoint.y - frame.minY)
        )
        window.contentView = trailView

        window.orderFront(nil)

        window.alphaValue = 0
        NSAnimationContext.runAnimationGroup({ context in
            context.duration = DesignLayout.fastAnimation
            window.animator().alphaValue = 1.0
        })

        DispatchQueue.main.asyncAfter(deadline: .now() + 0.7) { [weak window] in
            guard let window = window else { return }
            NSAnimationContext.runAnimationGroup({ context in
                context.duration = DesignLayout.standardAnimation
                window.animator().alphaValue = 0.0
            })
        }

        return window
    }

    // MARK: - Cleanup

    func closeAllOverlays() {
        queue.async { [weak self] in
            guard let self = self else { return }
            DispatchQueue.main.sync {
                self.borderWindows.forEach { $0.close() }
                self.borderWindows.removeAll()

                self.clickIndicators.forEach { $0.close() }
                self.clickIndicators.removeAll()

                self.moveTrails.forEach { $0.close() }
                self.moveTrails.removeAll()
            }
        }
    }
}
