import Foundation
import Testing
@testable import CalendarCore

/// Needs the simulator keychain, so lives in the app-hosted test target.
@Suite(.serialized) struct KeychainTests {
    @Test func roundTrip() throws {
        let kc = Keychain(service: "studio.thesunday.calendar.tests.\(UUID().uuidString)")
        defer { try? kc.delete("k") }
        #expect(try kc.get("k") == nil)
        try kc.set(Data("one".utf8), for: "k")
        try kc.set(Data("two".utf8), for: "k")
        #expect(try kc.get("k") == Data("two".utf8))
        try kc.delete("k")
        #expect(try kc.get("k") == nil)
    }

    @Test func hubConfigRoundTrip() throws {
        let store = HubConfigStore(keychain: Keychain(service: "studio.thesunday.calendar.tests.\(UUID().uuidString)"))
        #expect(store.load() == nil)
        let cfg = HubConfig(baseURL: URL(string: "https://hub.example")!, token: "cal_abc")
        try store.save(cfg)
        #expect(store.load() == cfg)
        try store.clear()
        #expect(store.load() == nil)
    }
}
