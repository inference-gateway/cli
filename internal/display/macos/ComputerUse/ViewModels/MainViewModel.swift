import Foundation
import Cocoa
import Combine

// MARK: - Content Item

enum ContentItem: Identifiable {
    case text(String, color: NSColor)
    case image(NSImage, name: String)

    var id: UUID { UUID() }
}

// MARK: - Main ViewModel
// Single source of truth for application state

class MainViewModel: ObservableObject {
    // MARK: - Published State

    @Published private(set) var isPaused = false
    @Published private(set) var currentRequestID: String?
    @Published private(set) var contentItems: [ContentItem] = []

    // MARK: - Dependencies

    private let outputWriter: OutputWriter
    private let windowCoordinator: WindowCoordinator

    // MARK: - Initialization

    init(outputWriter: OutputWriter, windowCoordinator: WindowCoordinator) {
        self.outputWriter = outputWriter
        self.windowCoordinator = windowCoordinator
    }

    // MARK: - Public Actions

    func pause() {
        NSLog("[MainViewModel] pause() called, currentRequestID: \(currentRequestID ?? "nil"), isPaused: \(isPaused)")
        guard let requestID = currentRequestID, !isPaused else {
            NSLog("[MainViewModel] Pause guard failed - requestID: \(currentRequestID ?? "nil"), isPaused: \(isPaused)")
            return
        }
        isPaused = true
        NSLog("[MainViewModel] Sending pause request for requestID: \(requestID)")
        outputWriter.sendPauseRequest(requestID: requestID)
        appendText("\n⏸ Execution paused\n", color: DesignColors.warning)
    }

    func resume() {
        NSLog("[MainViewModel] resume() called, currentRequestID: \(currentRequestID ?? "nil"), isPaused: \(isPaused)")
        guard let requestID = currentRequestID, isPaused else {
            NSLog("[MainViewModel] Resume guard failed - requestID: \(currentRequestID ?? "nil"), isPaused: \(isPaused)")
            return
        }
        isPaused = false
        NSLog("[MainViewModel] Sending resume request for requestID: \(requestID)")
        outputWriter.sendResumeRequest(requestID: requestID)
        appendText("\n▶ Resuming...\n", color: DesignColors.success)
    }

    // MARK: - Private Helpers

    private func appendText(_ text: String, color: NSColor) {
        var items = contentItems
        items.append(.text(text, color: color))
        contentItems = items
    }

    private func appendImage(_ attachment: ToolExecutionProgressEvent.ImageAttachment) {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self = self else {
                NSLog("[MainViewModel] Self deallocated during image decode")
                return
            }

            guard let data = Data(base64Encoded: attachment.data),
                  let image = NSImage(data: data) else {
                NSLog("[MainViewModel] Failed to decode image: \(attachment.displayName)")
                return
            }

            DispatchQueue.main.async { [weak self] in
                guard let self = self else {
                    NSLog("[MainViewModel] Self deallocated before image append")
                    return
                }
                self.contentItems.append(.image(image, name: attachment.displayName))
            }
        }
    }
}

// MARK: - Event Dispatcher Delegate

extension MainViewModel: EventDispatcherDelegate {
    func handleChatStart(_ event: ChatStartEvent) {
        currentRequestID = event.requestID
        appendText("\n● Starting chat with \(event.model)...\n", color: DesignColors.info)
    }

    func handleChatChunk(_ event: ChatChunkEvent) {
        if let content = event.content, !content.isEmpty {
            appendText(content, color: DesignColors.primaryText)
        }
    }

    func handleParallelToolsStart(_ event: ParallelToolsStartEvent) {
        let toolNames = event.tools.map { $0.name }.joined(separator: ", ")
        appendText("\n> Executing tools: \(toolNames)\n", color: DesignColors.info)
    }

    func handleToolExecutionProgress(_ event: ToolExecutionProgressEvent) {
        switch event.status.lowercased() {
        case "completed":
            appendText("✓ \(event.toolName) completed\n", color: DesignColors.success)

            if let images = event.images, !images.isEmpty {
                for image in images {
                    appendImage(image)
                }
            }

        case "failed":
            let message = event.message ?? "Unknown error"
            appendText("✗ \(event.toolName) failed: \(message)\n", color: DesignColors.error)

        case "running":
            appendText("⟳ \(event.toolName) running...\n", color: DesignColors.info)

        default:
            break
        }
    }

    func handleBorderOverlay(_ event: BorderOverlayEvent) {
        NSLog("[MainViewModel] Border overlay event received: \(event.borderAction)")
        if event.borderAction == "show" {
            windowCoordinator.showBorder()
        } else {
            windowCoordinator.hideBorder()
        }
    }

    func handleClickIndicator(_ event: ClickIndicatorEvent) {
        windowCoordinator.showClickIndicator(x: event.x, y: event.y)
    }

    func handleMoveIndicator(_ event: MoveIndicatorEvent) {
        windowCoordinator.showMoveTrail(
            from: (event.fromX, event.fromY),
            to: (event.toX, event.toY)
        )
    }

    func handleComputerUsePaused(_ event: ComputerUsePausedEvent) {
        isPaused = true
        appendText("\n⏸ Execution paused by system\n", color: DesignColors.warning)
    }

    func handleComputerUseResumed(_ event: ComputerUseResumedEvent) {
        isPaused = false
        appendText("\n▶ Execution resumed\n", color: DesignColors.success)
    }

    func handleToolApprovalNotification(_ event: ToolApprovalNotificationEvent) {
        appendText("! \(event.message)\n", color: DesignColors.warning)
    }
}
