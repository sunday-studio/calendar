import Foundation

/// Where the hub lives and the device token; stored in the shared Keychain.
public struct HubConfig: Codable, Hashable, Sendable {
    public var baseURL: URL
    public var token: String

    public init(baseURL: URL, token: String) {
        self.baseURL = baseURL
        self.token = token
    }
}

public struct HubConfigStore: Sendable {
    public static let defaultService = "studio.thesunday.calendar.hub"
    private let keychain: Keychain
    private let key = "hubConfig"

    public init(keychain: Keychain = Keychain(service: HubConfigStore.defaultService)) {
        self.keychain = keychain
    }

    public func load() -> HubConfig? {
        guard let data = try? keychain.get(key) else { return nil }
        return try? JSONDecoder().decode(HubConfig.self, from: data)
    }

    public func save(_ config: HubConfig) throws {
        try keychain.set(JSONEncoder().encode(config), for: key)
    }

    public func clear() throws { try keychain.delete(key) }
}
