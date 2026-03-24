import Foundation

struct Session: Codable, Identifiable, Hashable {
    let id: String
    let computerName: String
    let projectPath: String
    var name: String
    var status: String
    let createdAt: Date
    var lastSeenAt: Date
    var archived: Bool

    enum CodingKeys: String, CodingKey {
        case id
        case computerName = "computer_name"
        case projectPath = "project_path"
        case name
        case status
        case createdAt = "created_at"
        case lastSeenAt = "last_seen_at"
        case archived
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        computerName = try container.decode(String.self, forKey: .computerName)
        projectPath = try container.decode(String.self, forKey: .projectPath)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        status = try container.decode(String.self, forKey: .status)
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        lastSeenAt = try container.decode(Date.self, forKey: .lastSeenAt)
        archived = try container.decode(Bool.self, forKey: .archived)
    }

    var displayName: String {
        if !name.isEmpty { return name }
        let project = URL(fileURLWithPath: projectPath).lastPathComponent
        return "\(computerName) / \(project)"
    }

    var isStale: Bool {
        lastSeenAt.timeIntervalSinceNow < -300 // 5 minutes
    }
}
