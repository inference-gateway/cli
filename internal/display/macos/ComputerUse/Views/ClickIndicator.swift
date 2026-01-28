import Cocoa

// MARK: - Click Indicator View
// Renders a circular ring with a filled center to indicate click location

class ClickIndicatorView: NSView {
    override func draw(_ dirtyRect: NSRect) {
        guard let context = NSGraphicsContext.current?.cgContext else { return }

        context.setStrokeColor(DesignColors.clickRing.cgColor)
        context.setLineWidth(3)
        context.strokeEllipse(in: bounds.insetBy(dx: 2, dy: 2))

        let innerColor = DesignColors.clickRing.withAlphaComponent(0.3)
        context.setFillColor(innerColor.cgColor)
        context.fillEllipse(in: bounds.insetBy(dx: 12, dy: 12))
    }
}
