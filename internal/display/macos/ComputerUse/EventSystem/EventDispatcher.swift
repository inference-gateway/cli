import Foundation

// MARK: - Event Dispatcher Delegate Protocol

protocol EventDispatcherDelegate: AnyObject {
    func handleChatStart(_ event: ChatStartEvent)
    func handleChatChunk(_ event: ChatChunkEvent)
    func handleParallelToolsStart(_ event: ParallelToolsStartEvent)
    func handleToolExecutionProgress(_ event: ToolExecutionProgressEvent)
    func handleBorderOverlay(_ event: BorderOverlayEvent)
    func handleClickIndicator(_ event: ClickIndicatorEvent)
    func handleMoveIndicator(_ event: MoveIndicatorEvent)
    func handleComputerUsePaused(_ event: ComputerUsePausedEvent)
    func handleComputerUseResumed(_ event: ComputerUseResumedEvent)
    func handleToolApprovalNotification(_ event: ToolApprovalNotificationEvent)
}

// MARK: - Event Dispatcher

class EventDispatcher {
    weak var delegate: EventDispatcherDelegate?
    private let decoder: JSONDecoder

    init() {
        self.decoder = JSONDecoder()
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss.SSSSSSZZZZZ"
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        decoder.dateDecodingStrategy = .formatted(formatter)
    }

    // Main dispatch method - centralized JSON deserialization
    func dispatch(jsonData: Data) {
        guard let eventType = detectEventType(from: jsonData) else {
            if let jsonString = String(data: jsonData, encoding: .utf8) {
                NSLog("[EventDispatcher] Unknown event type. JSON: \(jsonString)")
            } else {
                NSLog("[EventDispatcher] Unknown event type")
            }
            return
        }

        guard delegate != nil else {
            NSLog("[EventDispatcher] Delegate is nil, dropping event: \(eventType)")
            return
        }

        do {
            switch eventType {
            case .chatStart:
                let event = try decoder.decode(ChatStartEvent.self, from: jsonData)
                delegate?.handleChatStart(event)

            case .chatChunk:
                let event = try decoder.decode(ChatChunkEvent.self, from: jsonData)
                delegate?.handleChatChunk(event)

            case .parallelToolsStart:
                let event = try decoder.decode(ParallelToolsStartEvent.self, from: jsonData)
                delegate?.handleParallelToolsStart(event)

            case .toolExecutionProgress:
                let event = try decoder.decode(ToolExecutionProgressEvent.self, from: jsonData)
                delegate?.handleToolExecutionProgress(event)

            case .borderOverlay:
                let event = try decoder.decode(BorderOverlayEvent.self, from: jsonData)
                delegate?.handleBorderOverlay(event)

            case .clickIndicator:
                let event = try decoder.decode(ClickIndicatorEvent.self, from: jsonData)
                delegate?.handleClickIndicator(event)

            case .moveIndicator:
                let event = try decoder.decode(MoveIndicatorEvent.self, from: jsonData)
                delegate?.handleMoveIndicator(event)

            case .computerUsePaused:
                let event = try decoder.decode(ComputerUsePausedEvent.self, from: jsonData)
                delegate?.handleComputerUsePaused(event)

            case .computerUseResumed:
                let event = try decoder.decode(ComputerUseResumedEvent.self, from: jsonData)
                delegate?.handleComputerUseResumed(event)

            case .toolApprovalNotification:
                let event = try decoder.decode(ToolApprovalNotificationEvent.self, from: jsonData)
                delegate?.handleToolApprovalNotification(event)
            }
        } catch {
            NSLog("[EventDispatcher] Failed to decode \(eventType): \(error)")
            if let jsonString = String(data: jsonData, encoding: .utf8) {
                NSLog("[EventDispatcher] JSON was: \(jsonString.prefix(200))")
            }
        }
    }

    // Event type detection using field presence heuristics
    // Order matters - more specific checks first
    private func detectEventType(from data: Data) -> EventType? {
        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }

        if json["BorderAction"] != nil {
            return .borderOverlay
        }

        if json["FromX"] != nil && json["FromY"] != nil &&
           json["ToX"] != nil && json["ToY"] != nil &&
           json["MoveIndicator"] as? Bool == true {
            return .moveIndicator
        }

        if json["X"] != nil && json["Y"] != nil &&
           json["ClickIndicator"] as? Bool == true {
            return .clickIndicator
        }

        if json["Tools"] != nil {
            return .parallelToolsStart
        }

        if json["ToolName"] != nil && json["Status"] != nil {
            return .toolExecutionProgress
        }

        if json["ToolName"] != nil && json["Message"] != nil &&
           json["Timestamp"] != nil && json["Status"] == nil {
            return .toolApprovalNotification
        }

        if json["Content"] != nil {
            return .chatChunk
        }

        if json["Model"] != nil {
            return .chatStart
        }

        if json["RequestID"] != nil && json["Timestamp"] != nil {
            NSLog("[EventDispatcher] Ambiguous event (RequestID + Timestamp only)")
            return nil
        }

        return nil
    }
}
