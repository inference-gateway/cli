import Cocoa

// MARK: - Image Enlarged Window
// Full-screen modal window for displaying enlarged screenshots

class ImageEnlargedWindow: NSWindow {
    init(image: NSImage) {
        let screen = NSScreen.main?.frame ?? NSRect(x: 0, y: 0, width: 1920, height: 1080)

        super.init(
            contentRect: screen,
            styleMask: .borderless,
            backing: .buffered,
            defer: false
        )

        backgroundColor = NSColor.black.withAlphaComponent(0.9)
        level = .modalPanel
        isOpaque = false

        let imageView = NSImageView(frame: screen)
        imageView.image = image
        imageView.imageScaling = .scaleProportionallyDown

        let clickGesture = NSClickGestureRecognizer(target: self, action: #selector(closeWindow))
        imageView.addGestureRecognizer(clickGesture)

        contentView = imageView
    }

    @objc private func closeWindow() {
        close()
    }
}
