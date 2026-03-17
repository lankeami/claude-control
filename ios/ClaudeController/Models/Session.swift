import Foundation

struct Session: Codable, Identifiable, Hashable {
    let id: String
    let computerName: String
    let projectPath: String
    var status: String
    let createdAt: Date
    var lastSeenAt: Date
    var archived: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case computerName = "computer_name"
        case projectPath = "project_path"
        case status
        case createdAt = "created_at"
        case lastSeenAt = "last_seen_at"
        case archived
    }

    var displayName: String {
        let project = URL(fileURLWithPath: projectPath).lastPathComponent
        return "\(computerName) / \(project)"
    }

    var isStale: Bool {
        lastSeenAt.timeIntervalSinceNow < -300 // 5 minutes
    }
}
