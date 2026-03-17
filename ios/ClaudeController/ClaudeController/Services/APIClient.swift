import Foundation
import Combine

class APIClient: ObservableObject {
    private var config: ServerConfig
    private let session: URLSession
    private let decoder: JSONDecoder

    init(config: ServerConfig) {
        self.config = config
        self.session = URLSession.shared
        self.decoder = JSONDecoder()
        self.decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let dateStr = try container.decode(String.self)
            let formatter = ISO8601DateFormatter()
            formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = formatter.date(from: dateStr) { return date }
            // Try SQLite datetime format
            let sqlFormatter = DateFormatter()
            sqlFormatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
            sqlFormatter.timeZone = TimeZone(identifier: "UTC")
            if let date = sqlFormatter.date(from: dateStr) { return date }
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "Cannot decode date: \(dateStr)")
        }
    }

    func updateConfig(_ config: ServerConfig) {
        self.config = config
    }

    private func request(_ method: String, _ path: String, body: Data? = nil) async throws -> Data {
        guard let url = URL(string: config.url + path) else {
            throw APIError.invalidURL
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.setValue("Bearer \(config.key)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = body
        req.timeoutInterval = 10

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200...299).contains(http.statusCode) else {
            throw APIError.httpError(http.statusCode)
        }
        return data
    }

    // MARK: - Pairing

    func validatePairing() async throws -> Bool {
        let _ = try await request("GET", "/api/pairing")
        return true
    }

    func checkStatus() async throws -> Bool {
        let _ = try await request("GET", "/api/status")
        return true
    }

    // MARK: - Sessions

    func listSessions(includeArchived: Bool = false) async throws -> [Session] {
        let path = includeArchived ? "/api/sessions?include_archived=true" : "/api/sessions"
        let data = try await request("GET", path)
        return try decoder.decode([Session].self, from: data)
    }

    func setArchived(sessionId: String, archived: Bool) async throws {
        let body = try JSONEncoder().encode(["archived": archived])
        let _ = try await request("PUT", "/api/sessions/\(sessionId)/archive", body: body)
    }

    // MARK: - Prompts

    func listPrompts(sessionId: String? = nil, status: String? = nil) async throws -> [Prompt] {
        var path = "/api/prompts?"
        if let sid = sessionId { path += "session_id=\(sid)&" }
        if let s = status { path += "status=\(s)&" }
        let data = try await request("GET", path)
        return try decoder.decode([Prompt].self, from: data)
    }

    func respondToPrompt(promptId: String, response: String) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["response": response])
        let _ = try await request("POST", "/api/prompts/\(promptId)/respond", body: body)
    }

    // MARK: - Instructions

    func sendInstruction(sessionId: String, message: String) async throws {
        let body = try JSONSerialization.data(withJSONObject: ["message": message])
        let _ = try await request("POST", "/api/sessions/\(sessionId)/instruct", body: body)
    }
}

enum APIError: LocalizedError {
    case invalidURL
    case invalidResponse
    case httpError(Int)

    var errorDescription: String? {
        switch self {
        case .invalidURL: return "Invalid server URL"
        case .invalidResponse: return "Invalid response from server"
        case .httpError(let code): return "Server returned error \(code)"
        }
    }
}
