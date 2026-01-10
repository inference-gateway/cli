import Cocoa
import Combine

// MARK: - Main Window
// Floating panel with transparency, auto-scroll content, and pause/resume controls

class MainWindow: NSPanel {
    // MARK: - Properties

    private let viewModel: MainViewModel
    private var cancellables = Set<AnyCancellable>()

    private var outerScrollView: NSScrollView!
    private var contentContainer: NSView!
    private var textView: NSTextView!
    private var controlBar: NSView!
    private var pauseButton: NSButton!
    private var resumeButton: NSButton!

    // MARK: - Initialization

    init(position: String, alwaysOnTop: Bool, viewModel: MainViewModel) {
        self.viewModel = viewModel

        let frame = NSRect(
            x: 0,
            y: 0,
            width: DesignWindow.defaultWidth,
            height: DesignWindow.defaultHeight
        )

        super.init(
            contentRect: frame,
            styleMask: [.titled, .miniaturizable],
            backing: .buffered,
            defer: false
        )

        self.title = "ComputerUse"
        self.level = alwaysOnTop ? .floating : .normal
        self.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary]
        self.hidesOnDeactivate = false
        self.backgroundColor = DesignColors.windowBackground
        self.isOpaque = false
        self.alphaValue = DesignWindow.windowAlpha

        setupUI()
        positionWindow(position: position)
        bindViewModel()
    }

    // MARK: - UI Setup

    private func setupUI() {
        let containerView = NSView(frame: contentView!.bounds)
        containerView.autoresizingMask = [.width, .height]

        controlBar = NSView(frame: NSRect(
            x: 0,
            y: 0,
            width: containerView.bounds.width,
            height: DesignLayout.controlHeight
        ))
        controlBar.wantsLayer = true
        controlBar.layer?.backgroundColor = DesignColors.controlBackground.cgColor
        controlBar.autoresizingMask = [.width, .maxYMargin]

        pauseButton = NSButton(frame: NSRect(
            x: DesignLayout.padding,
            y: (DesignLayout.controlHeight - DesignLayout.buttonHeight) / 2,
            width: 100,
            height: DesignLayout.buttonHeight
        ))
        pauseButton.title = "⏸ Pause"
        pauseButton.bezelStyle = .rounded
        pauseButton.target = self
        pauseButton.action = #selector(togglePauseResume)

        controlBar.addSubview(pauseButton)

        let scrollFrame = NSRect(
            x: 0,
            y: DesignLayout.controlHeight,
            width: containerView.bounds.width,
            height: containerView.bounds.height - DesignLayout.controlHeight
        )
        outerScrollView = NSScrollView(frame: scrollFrame)
        outerScrollView.autoresizingMask = [.width, .height]
        outerScrollView.hasVerticalScroller = true
        outerScrollView.drawsBackground = false

        textView = NSTextView(frame: outerScrollView.bounds)
        textView.isEditable = false
        textView.isSelectable = true
        textView.drawsBackground = false
        textView.font = DesignFonts.body
        textView.textColor = DesignColors.primaryText
        textView.textContainerInset = NSSize(width: DesignLayout.padding, height: DesignLayout.padding)
        textView.autoresizingMask = [.width]

        outerScrollView.documentView = textView
        contentContainer = textView

        containerView.addSubview(outerScrollView)
        containerView.addSubview(controlBar)

        contentView = containerView
    }

    private func positionWindow(position: String) {
        guard let screen = NSScreen.main else { return }

        let screenFrame = screen.visibleFrame
        var windowOrigin: CGPoint

        switch position {
        case "top-left":
            windowOrigin = CGPoint(
                x: screenFrame.minX + 20,
                y: screenFrame.maxY - frame.height - 20
            )
        case "top-right":
            windowOrigin = CGPoint(
                x: screenFrame.maxX - frame.width - 20,
                y: screenFrame.maxY - frame.height - 20
            )
        default:
            windowOrigin = CGPoint(
                x: screenFrame.maxX - frame.width - 20,
                y: screenFrame.maxY - frame.height - 20
            )
        }

        setFrameOrigin(windowOrigin)
    }

    // MARK: - View Model Binding

    private func bindViewModel() {
        viewModel.$contentItems
            .receive(on: DispatchQueue.main)
            .sink { [weak self] items in
                self?.updateContent(items)
            }
            .store(in: &cancellables)

        viewModel.$isPaused
            .receive(on: DispatchQueue.main)
            .sink { [weak self] isPaused in
                self?.pauseButton.title = isPaused ? "▶ Resume" : "⏸ Pause"
            }
            .store(in: &cancellables)
    }

    // MARK: - Content Updates

    private func updateContent(_ items: [ContentItem]) {
        let textContent = NSMutableAttributedString()

        for item in items {
            switch item {
            case .text(let text, let color):
                let attributes: [NSAttributedString.Key: Any] = [
                    .font: DesignFonts.body,
                    .foregroundColor: color
                ]
                textContent.append(NSAttributedString(string: text, attributes: attributes))

            case .image(let image, _):
                textContent.append(NSAttributedString(string: "\n"))

                let attachment = NSTextAttachment()
                attachment.image = image

                let maxWidth = textView.bounds.width - (DesignLayout.padding * 2) - 20
                let imageSize = image.size
                let scale = min(maxWidth / imageSize.width, 1.0)
                let scaledSize = NSSize(
                    width: imageSize.width * scale,
                    height: imageSize.height * scale
                )
                attachment.bounds = NSRect(origin: .zero, size: scaledSize)

                let imageString = NSAttributedString(attachment: attachment)
                textContent.append(imageString)

                textContent.append(NSAttributedString(string: "\n"))
            }
        }

        textView.textStorage?.setAttributedString(textContent)

        DispatchQueue.main.async { [weak self] in
            self?.scrollToBottom()
        }
    }

    private func scrollToBottom() {
        textView.scrollToEndOfDocument(nil)
    }

    // MARK: - Actions

    @objc private func togglePauseResume() {
        if viewModel.isPaused {
            viewModel.resume()
        } else {
            viewModel.pause()
        }
    }

    @objc private func imageClicked(_ sender: NSClickGestureRecognizer) {
        guard let imageView = sender.view as? NSImageView,
              let image = imageView.image else { return }

        let enlargedWindow = ImageEnlargedWindow(image: image)
        enlargedWindow.makeKeyAndOrderFront(nil)
    }
}
