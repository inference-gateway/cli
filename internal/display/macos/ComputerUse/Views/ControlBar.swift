import Cocoa

// MARK: - Control Bar View
// Bottom bar with pause/resume button

class ControlBar: NSView {
    // MARK: - Properties

    private let pauseButton: NSButton
    private let pauseAction: () -> Void

    // MARK: - Initialization

    init(frame: NSRect, pauseAction: @escaping () -> Void) {
        self.pauseAction = pauseAction

        self.pauseButton = NSButton(frame: NSRect(
            x: DesignLayout.padding,
            y: (DesignLayout.controlHeight - DesignLayout.buttonHeight) / 2,
            width: 100,
            height: DesignLayout.buttonHeight
        ))

        super.init(frame: frame)

        setupUI()
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    // MARK: - Setup

    private func setupUI() {
        wantsLayer = true
        layer?.backgroundColor = DesignColors.controlBackground.cgColor
        autoresizingMask = [.width, .maxYMargin]

        pauseButton.title = "⏸ Pause"
        pauseButton.bezelStyle = .rounded
        pauseButton.target = self
        pauseButton.action = #selector(handlePauseAction)

        addSubview(pauseButton)
    }

    // MARK: - Public Methods

    func updateButtonTitle(isPaused: Bool) {
        pauseButton.title = isPaused ? "▶ Resume" : "⏸ Pause"
    }

    // MARK: - Actions

    @objc private func handlePauseAction() {
        pauseAction()
    }
}
