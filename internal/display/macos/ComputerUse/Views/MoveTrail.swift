import Cocoa

// MARK: - Move Trail View
// Renders an arrow showing mouse movement from one point to another

class MoveTrailView: NSView {
    let fromPoint: CGPoint
    let toPoint: CGPoint

    init(frame frameRect: NSRect, fromPoint: CGPoint, toPoint: CGPoint) {
        self.fromPoint = fromPoint
        self.toPoint = toPoint
        super.init(frame: frameRect)
    }

    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let context = NSGraphicsContext.current?.cgContext else { return }

        context.setStrokeColor(DesignColors.moveTrail.cgColor)
        context.setLineWidth(3)
        context.setLineCap(.round)

        context.beginPath()
        context.move(to: fromPoint)
        context.addLine(to: toPoint)
        context.strokePath()

        let angle = atan2(toPoint.y - fromPoint.y, toPoint.x - fromPoint.x)
        let arrowLength: CGFloat = 15
        let arrowAngle: CGFloat = .pi / 6

        let point1 = CGPoint(
            x: toPoint.x - arrowLength * cos(angle - arrowAngle),
            y: toPoint.y - arrowLength * sin(angle - arrowAngle)
        )
        let point2 = CGPoint(
            x: toPoint.x - arrowLength * cos(angle + arrowAngle),
            y: toPoint.y - arrowLength * sin(angle + arrowAngle)
        )

        context.beginPath()
        context.move(to: point1)
        context.addLine(to: toPoint)
        context.addLine(to: point2)
        context.strokePath()
    }
}
