import Foundation

struct ServerConfig: Codable, Identifiable, Hashable {
    let url: String
    let key: String
    let version: Int
    var label: String? // User-assigned label, e.g. computer name

    var id: String { url }
}
