import Foundation

// MARK: - Event Reader

/// Reads newline-delimited JSON events from stdin on a background thread
class EventReader {
    private let dispatcher: EventDispatcher

    init(dispatcher: EventDispatcher) {
        self.dispatcher = dispatcher
    }

    /// Start reading events from stdin on a background thread
    func startReading() {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self = self else { return }

            let handle = FileHandle.standardInput

            while true {
                var lineData = Data()

                while true {
                    do {
                        guard let byte = try handle.read(upToCount: 1), !byte.isEmpty else {
                            NSLog("[EventReader] EOF reached, stopping")
                            return
                        }

                        if byte[0] == 10 {
                            break
                        }

                        lineData.append(byte[0])
                    } catch {
                        NSLog("[EventReader] Read error: \(error)")
                        return
                    }
                }

                if !lineData.isEmpty {
                    self.processEvent(data: lineData)
                }
            }
        }
    }

    private func processEvent(data: Data) {
        DispatchQueue.main.async { [weak self] in
            self?.dispatcher.dispatch(jsonData: data)
        }
    }
}
