import Foundation

// MARK: - Event Protocol

protocol ChatEvent: Codable {
    var requestID: String { get }
    var timestamp: Date { get }
}

// MARK: - Event Types

enum EventType {
    case chatStart
    case chatChunk
    case parallelToolsStart
    case toolExecutionProgress
    case borderOverlay
    case clickIndicator
    case moveIndicator
    case computerUsePaused
    case computerUseResumed
    case toolApprovalNotification
}

// MARK: - Event Models

// 1. Chat Start Event
struct ChatStartEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let model: String

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case model = "Model"
    }
}

// 2. Chat Chunk Event (streaming text)
struct ChatChunkEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let content: String?

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case content = "Content"
    }
}

// 3. Parallel Tools Start Event
struct ParallelToolsStartEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let tools: [ToolInfo]

    struct ToolInfo: Codable {
        let callID: String
        let name: String
        let status: String
        let arguments: String

        enum CodingKeys: String, CodingKey {
            case callID = "CallID"
            case name = "Name"
            case status = "Status"
            case arguments = "Arguments"
        }
    }

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case tools = "Tools"
    }
}

// 4. Tool Execution Progress Event (with screenshots)
struct ToolExecutionProgressEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let toolName: String
    let status: String
    let message: String?
    let images: [ImageAttachment]?

    struct ImageAttachment: Codable {
        let data: String
        let mimeType: String
        let displayName: String

        enum CodingKeys: String, CodingKey {
            case data
            case mimeType = "mime_type"
            case displayName = "display_name"
        }
    }

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case toolName = "ToolName"
        case status = "Status"
        case message = "Message"
        case images = "Images"
    }
}

// 5. Border Overlay Event
struct BorderOverlayEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let borderAction: String

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case borderAction = "BorderAction"
    }
}

// 6. Click Indicator Event
struct ClickIndicatorEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let x: Int
    let y: Int
    let clickIndicator: Bool

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case x = "X"
        case y = "Y"
        case clickIndicator = "ClickIndicator"
    }
}

// 7. Move Indicator Event
struct MoveIndicatorEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let fromX: Int
    let fromY: Int
    let toX: Int
    let toY: Int
    let moveIndicator: Bool

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case fromX = "FromX"
        case fromY = "FromY"
        case toX = "ToX"
        case toY = "ToY"
        case moveIndicator = "MoveIndicator"
    }
}

// 8. Computer Use Paused Event
struct ComputerUsePausedEvent: ChatEvent {
    let requestID: String
    let timestamp: Date

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
    }
}

// 9. Computer Use Resumed Event
struct ComputerUseResumedEvent: ChatEvent {
    let requestID: String
    let timestamp: Date

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
    }
}

// 10. Tool Approval Notification Event
struct ToolApprovalNotificationEvent: ChatEvent {
    let requestID: String
    let timestamp: Date
    let toolName: String
    let message: String

    enum CodingKeys: String, CodingKey {
        case requestID = "RequestID"
        case timestamp = "Timestamp"
        case toolName = "ToolName"
        case message = "Message"
    }
}

// MARK: - Pause/Resume Request (Swift → Go)

struct PauseResumeRequest: Codable {
    let action: String
    let request_id: String
}
