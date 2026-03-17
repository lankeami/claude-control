import Foundation
import Security

class KeychainService {
    private static let serviceKey = "com.claude-controller.servers"

    static func saveConfigs(_ configs: [ServerConfig]) {
        guard let data = try? JSONEncoder().encode(configs) else { return }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
        ]

        SecItemDelete(query as CFDictionary)

        let addQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
            kSecValueData as String: data,
        ]

        SecItemAdd(addQuery as CFDictionary, nil)
    }

    static func loadConfigs() -> [ServerConfig] {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: serviceKey,
            kSecReturnData as String: true,
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess, let data = result as? Data else {
            return []
        }

        return (try? JSONDecoder().decode([ServerConfig].self, from: data)) ?? []
    }

    static func addConfig(_ config: ServerConfig) {
        var configs = loadConfigs()
        configs.removeAll { $0.url == config.url }
        configs.append(config)
        saveConfigs(configs)
    }

    static func removeConfig(url: String) {
        var configs = loadConfigs()
        configs.removeAll { $0.url == url }
        saveConfigs(configs)
    }
}
