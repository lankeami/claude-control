import Foundation
import Combine

@MainActor
class PollingService: ObservableObject {
    @Published var sessions: [Session] = []
    @Published var pendingPrompts: [Prompt] = []
    @Published var allPrompts: [Prompt] = []
    @Published var isConnected: Bool = false
    @Published var selectedSessionId: String?

    private var apiClient: APIClient?
    private var timer: Timer?
    private var activeInterval: TimeInterval = 3
    private var idleInterval: TimeInterval = 15

    func configure(client: APIClient) {
        self.apiClient = client
        startPolling()
    }

    func startPolling() {
        stopPolling()
        poll()
        scheduleNext()
    }

    func stopPolling() {
        timer?.invalidate()
        timer = nil
    }

    private func scheduleNext() {
        let hasActiveSession = sessions.contains { $0.status == "waiting" || $0.status == "active" }
        let interval = hasActiveSession ? activeInterval : idleInterval

        timer = Timer.scheduledTimer(withTimeInterval: interval, repeats: false) { [weak self] _ in
            Task { @MainActor in
                self?.poll()
                self?.scheduleNext()
            }
        }
    }

    private func poll() {
        guard let client = apiClient else { return }

        Task {
            do {
                self.sessions = try await client.listSessions()
                self.pendingPrompts = try await client.listPrompts(status: "pending")

                if let sid = selectedSessionId {
                    self.allPrompts = try await client.listPrompts(sessionId: sid)
                }

                self.isConnected = true
            } catch {
                self.isConnected = false
            }
        }
    }

    var pendingCount: Int { pendingPrompts.count }
}
