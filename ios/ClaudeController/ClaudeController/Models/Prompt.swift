import Foundation

struct Prompt: Codable, Identifiable {
    let id: String
    let sessionId: String
    let claudeMessage: String
    let type: String // "prompt" or "notification"
    var response: String?
    var status: String // "pending" or "answered"
    let createdAt: Date
    var answeredAt: Date?

    enum CodingKeys: String, CodingKey {
        case id
        case sessionId = "session_id"
        case claudeMessage = "claude_message"
        case type
        case response
        case status
        case createdAt = "created_at"
        case answeredAt = "answered_at"
    }

    var isPending: Bool { status == "pending" && type == "prompt" }
    var isNotification: Bool { type == "notification" }
}
