import Cocoa

// MARK: - Design System
// Native macOS colors, fonts, and layout constants for professional appearance

// MARK: - Colors

struct DesignColors {
    // MARK: Text Colors
    static let primaryText = NSColor.labelColor
    static let secondaryText = NSColor.secondaryLabelColor
    static let tertiaryText = NSColor.tertiaryLabelColor

    // MARK: Status Colors
    static let success = NSColor.systemGreen
    static let error = NSColor.systemRed
    static let warning = NSColor.systemOrange
    static let info = NSColor.systemBlue

    // MARK: Overlay Colors
    static let borderHighlight = NSColor.systemBlue.withAlphaComponent(0.9)
    static let clickRing = NSColor.systemBlue.withAlphaComponent(0.8)
    static let moveTrail = NSColor.systemTeal.withAlphaComponent(0.7)

    // MARK: Window Colors
    static let windowBackground = NSColor.windowBackgroundColor.withAlphaComponent(0.95)
    static let controlBackground = NSColor.controlBackgroundColor.withAlphaComponent(0.90)
    static let separatorColor = NSColor.separatorColor

    // MARK: Interactive Colors
    static let buttonBackground = NSColor.controlColor
    static let buttonHighlight = NSColor.selectedContentBackgroundColor
}

// MARK: - Fonts

struct DesignFonts {
    // MARK: System Fonts
    static let body = NSFont.systemFont(ofSize: 13, weight: .regular)
    static let bodyBold = NSFont.systemFont(ofSize: 13, weight: .semibold)
    static let caption = NSFont.systemFont(ofSize: 11, weight: .regular)
    static let title = NSFont.systemFont(ofSize: 14, weight: .semibold)

    // MARK: Monospace Fonts
    static let mono = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
    static let monoBold = NSFont.monospacedSystemFont(ofSize: 12, weight: .semibold)
}

// MARK: - Layout

struct DesignLayout {
    // MARK: Spacing
    static let tinySpacing: CGFloat = 4
    static let smallSpacing: CGFloat = 8
    static let mediumSpacing: CGFloat = 12
    static let largeSpacing: CGFloat = 16
    static let extraLargeSpacing: CGFloat = 24

    // MARK: Padding
    static let padding: CGFloat = 12
    static let contentPadding: CGFloat = 16

    // MARK: Sizes
    static let controlHeight: CGFloat = 44
    static let buttonHeight: CGFloat = 32
    static let thumbnailWidth: CGFloat = 200
    static let thumbnailHeight: CGFloat = 150
    static let thumbnailSize = CGSize(width: thumbnailWidth, height: thumbnailHeight)
    static let minimizedWidth: CGFloat = 40
    static let borderWidth: CGFloat = 8
    static let clickIndicatorSize: CGFloat = 40

    // MARK: Radii
    static let cornerRadius: CGFloat = 8
    static let smallCornerRadius: CGFloat = 4

    // MARK: Animation Durations
    static let fastAnimation: TimeInterval = 0.15
    static let standardAnimation: TimeInterval = 0.25
    static let slowAnimation: TimeInterval = 0.4
    static let clickIndicatorDuration: TimeInterval = 0.7
    static let moveTrailDuration: TimeInterval = 1.0
}

// MARK: - Window Configuration

struct DesignWindow {
    static let defaultWidth: CGFloat = 450
    static let defaultHeight: CGFloat = 600
    static let minimumWidth: CGFloat = 300
    static let minimumHeight: CGFloat = 200
    static let windowAlpha: CGFloat = 0.95
    static let backgroundAlpha: CGFloat = 0.90
}
