import Foundation

// MARK: - Output Writer
// Handles communication from Swift → Go via stdout

class OutputWriter {
    private let encoder: JSONEncoder

    init() {
        self.encoder = JSONEncoder()
    }

    /// Send a pause request to Go backend
    func sendPauseRequest(requestID: String) {
        send(PauseResumeRequest(action: "pause", request_id: requestID))
    }

    /// Send a resume request to Go backend
    func sendResumeRequest(requestID: String) {
        send(PauseResumeRequest(action: "resume", request_id: requestID))
    }

    private func send(_ request: PauseResumeRequest) {
        do {
            let jsonData = try encoder.encode(request)
            guard let jsonString = String(data: jsonData, encoding: .utf8) else {
                NSLog("[OutputWriter] Failed to convert JSON to string")
                return
            }

            NSLog("[OutputWriter] About to print JSON: \(jsonString)")
            print(jsonString)
            fflush(stdout)

            NSLog("[OutputWriter] Sent: \(request.action) for \(request.request_id)")
        } catch {
            NSLog("[OutputWriter] Failed to encode request: \(error)")
        }
    }
}
