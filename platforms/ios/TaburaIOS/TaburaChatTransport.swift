import Foundation

final class TaburaChatTransport {
    private let session: URLSession
    private let decoder = JSONDecoder()
    private var task: URLSessionWebSocketTask?
    private let onEvent: @MainActor (TaburaChatEventPayload) -> Void
    private let onDisconnect: @MainActor (String) -> Void

    init(session: URLSession, onEvent: @escaping @MainActor (TaburaChatEventPayload) -> Void, onDisconnect: @escaping @MainActor (String) -> Void) {
        self.session = session
        self.onEvent = onEvent
        self.onDisconnect = onDisconnect
    }

    func connect(baseURL: URL, sessionID: String) {
        disconnect()
        guard let wsURL = taburaWSURL(baseURL: baseURL, path: "chat/\(sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID)") else {
            return
        }
        let task = session.webSocketTask(with: wsURL)
        self.task = task
        task.resume()
        receiveLoop(task: task)
    }

    func disconnect() {
        task?.cancel(with: .normalClosure, reason: nil)
        task = nil
    }

    func send<E: Encodable>(_ payload: E) async throws {
        guard let task else {
            throw URLError(.networkConnectionLost)
        }
        let data = try JSONEncoder().encode(payload)
        guard let text = String(data: data, encoding: .utf8) else {
            throw NSError(domain: NSURLErrorDomain, code: -1, userInfo: [NSLocalizedDescriptionKey: "websocket payload encoding failed"])
        }
        try await task.send(.string(text))
    }

    private func receiveLoop(task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case let .success(message):
                self.handle(message: message)
                self.receiveLoop(task: task)
            case let .failure(error):
                Task { @MainActor in
                    self.onDisconnect(error.localizedDescription)
                }
            }
        }
    }

    private func handle(message: URLSessionWebSocketTask.Message) {
        let data: Data
        switch message {
        case let .data(raw):
            data = raw
        case let .string(raw):
            data = Data(raw.utf8)
        @unknown default:
            return
        }
        guard let payload = try? decoder.decode(TaburaChatEventPayload.self, from: data) else {
            return
        }
        Task { @MainActor in
            self.onEvent(payload)
        }
    }
}
