import CalendarCore
import Foundation
import Testing
@testable import FeatureAccounts
@testable import FeatureSettings

private let now = Date(timeIntervalSince1970: 1_791_800_000)
private func tok(_ id: String, _ name: String, kind: TokenKind = .script, scope: TokenScope = .read, used: TimeInterval? = nil,
                 revoked: TimeInterval? = nil, created: TimeInterval = 0) -> APIToken {
    APIToken(id: id, name: name, kind: kind, scope: scope, prefix: "cal_\(id)", createdAt: now.addingTimeInterval(-created),
             lastUsedAt: used.map { now.addingTimeInterval(-$0) }, revokedAt: revoked.map { now.addingTimeInterval(-$0) })
}

private final class FakeTokens: HubTokensAPI, @unchecked Sendable {
    var list: Result<[APIToken], Error> = .success([])
    var create: Result<CreatedToken, Error> = .success(CreatedToken(token: tok("new", "Home script"), secret: "cal_secret_value"))
    var revokeError: Error?
    var me = "t-me"
    private(set) var revoked: [String] = []
    private(set) var created: [(String, TokenKind, TokenScope)] = []
    func hello() async throws -> Hello { Hello(server: "calendard", version: "1", apiVersion: 1, scope: "admin", tokenId: me) }
    func tokens() async throws -> [APIToken] { try list.get() }
    func createToken(name: String, kind: TokenKind, scope: TokenScope) async throws -> CreatedToken { created.append((name, kind, scope)); return try create.get() }
    func revokeToken(id: String) async throws { if let revokeError { throw revokeError }; revoked.append(id) }
}

@MainActor @Suite struct TokensTests {
    @Test func rowWordsCoverScopeKindLastUsedAndRevoked() {
        #expect(TokensViewModel.subLine(tok("a", "A", scope: .write, used: 30), now: now) == "Read and write \u{00B7} used just now")
        #expect(TokensViewModel.subLine(tok("a", "A", scope: .read, used: 9 * 86_400), now: now) == "Read only \u{00B7} not used in 9 days")
        #expect(TokensViewModel.subLine(tok("a", "A", used: 3 * 3600), now: now) == "Read only \u{00B7} used 3 h ago")
        #expect(TokensViewModel.subLine(tok("a", "A", used: 86_400 + 10), now: now) == "Read only \u{00B7} used yesterday")
        #expect(TokensViewModel.subLine(tok("a", "A"), now: now) == "Read only \u{00B7} never used")
        #expect(TokensViewModel.subLine(tok("a", "A", scope: .admin, revoked: 2 * 86_400), now: now) == "Full access \u{00B7} revoked 2 days ago")
    }

    @Test func loadSplitsActiveAndRevokedAndMarksThisPhone() async {
        let api = FakeTokens()
        api.list = .success([tok("t-me", "This phone", kind: .device, scope: .admin, used: 5, created: 10), tok("s1", "Script", created: 20), tok("old", "Old", revoked: 100, created: 30)])
        let m = TokensViewModel(api: api, now: { now })
        await m.load()
        #expect(m.phase == .loaded && m.active.map(\.id) == ["t-me", "s1"] && m.revoked.map(\.id) == ["old"])
        #expect(m.active[0].isMine && !m.active[1].isMine && m.revoked[0].revoked)
    }

    @Test func nonAdminTokenGetsAPlainExplanationNotAnError() async {
        let api = FakeTokens(); api.list = .failure(APIClientError.unauthorized(APIError.Body(code: "forbidden", message: "admin scope required").asError))
        let m = TokensViewModel(api: api)
        await m.load()
        #expect(m.phase == .needsAdmin)
    }

    @Test func offlineAndRejectedTokenWords() async {
        let api = FakeTokens(); api.list = .failure(APIClientError.transport("x"))
        let m = TokensViewModel(api: api)
        await m.load()
        #expect(m.phase == .failed("Can\u{2019}t reach the server. Check Tailscale, then try again."))
        api.list = .failure(APIClientError.unauthorized(nil))
        await m.load()
        #expect(m.phase == .failed("The server no longer accepts this phone\u{2019}s token."))
    }

    @Test func revokeReloadsAndRefusesTheTokenInUse() async {
        let api = FakeTokens()
        api.list = .success([tok("t-me", "This phone"), tok("s1", "Script")])
        let m = TokensViewModel(api: api, now: { now })
        await m.load()
        #expect(await m.revoke("t-me") == false && api.revoked.isEmpty && m.message != nil)
        api.list = .success([tok("t-me", "This phone"), tok("s1", "Script", revoked: 1)])
        #expect(await m.revoke("s1") && api.revoked == ["s1"] && m.revoked.map(\.id) == ["s1"] && m.message == nil)
    }

    @Test func revokeFailureLeavesTheListAlone() async {
        let api = FakeTokens(); api.list = .success([tok("s1", "Script")])
        let m = TokensViewModel(api: api); await m.load()
        api.revokeError = APIClientError.unauthorized(APIError.Body(code: "forbidden", message: "no").asError)
        #expect(await m.revoke("s1") == false && m.message == "This token can\u{2019}t revoke tokens. Use a token with admin access.")
        #expect(m.active.map(\.id) == ["s1"])
    }

    @Test func newTokenShowsTheSecretOnceAndForgetsItOnDone() async {
        let api = FakeTokens()
        nonisolated(unsafe) var copied: [String] = []
        let m = NewTokenViewModel(api: api, copy: { copied.append($0) })
        #expect(!m.canCreate)
        m.name = "  Home script  "
        #expect(m.canCreate && m.scope == .read)
        await m.create()
        #expect(api.created.count == 1 && api.created[0] == ("Home script", .script, .read))
        #expect(m.secret == "cal_secret_value")
        m.copyToken()
        #expect(copied == ["cal_secret_value"] && m.copied)
        m.done()
        #expect(m.secret == nil && m.phase == .editing && m.name.isEmpty)
        m.copyToken()
        #expect(copied.count == 1)                                     // nothing left to copy
    }

    @Test func writeAccessAndDeviceKindAreSent() async {
        let api = FakeTokens()
        let m = NewTokenViewModel(api: api, copy: { _ in })
        m.name = "Tablet"; m.kind = .device; m.canWrite = true
        await m.create()
        #expect(api.created[0] == ("Tablet", .device, .write))
    }

    @Test func createErrorsStayOnTheForm() async {
        let api = FakeTokens(); api.create = .failure(APIClientError.unauthorized(APIError.Body(code: "forbidden", message: "no").asError))
        let m = NewTokenViewModel(api: api, copy: { _ in })
        m.name = "x"
        await m.create()
        #expect(m.phase == .editing && m.error == "This token can\u{2019}t make tokens. Use a token with admin access." && m.secret == nil)
        m.name = "xy"
        #expect(m.error == nil)                                        // editing clears the message
    }
}

private extension APIError.Body { var asError: APIError { APIError(error: self) } }

// MARK: Read-only link

@MainActor @Suite struct IcsLinkTests {
    private func hub() -> FakeHub { FakeHub() }

    @Test func addsAndReportsTheAccount() async {
        let h = hub()
        let m = IcsLinkViewModel(api: h)
        m.url = "  webcal://feeds.example.com/holidays.ics "
        #expect(m.canAdd)
        await m.add()
        guard case .added(let a) = m.phase else { Issue.record("not added: \(m.phase)"); return }
        #expect(a.provider == "ics" && h.calls == ["ics:webcal://feeds.example.com/holidays.ics"])
    }

    @Test func hub422OnUrlShowsInlineAndSavesNothing() async {
        let h = hub()
        h.icsResult = .failure(APIClientError.server(status: 422, APIError(error: .init(code: "validation", message: "That link did not return a calendar.", field: "url"))))
        let m = IcsLinkViewModel(api: h)
        m.url = "https://example.com/not-a-calendar"
        await m.add()
        #expect(m.phase == .editing && m.urlError == "That link did not return a calendar." && m.banner == nil)
        m.url = "https://example.com/other.ics"
        #expect(m.urlError == nil)                                      // typing clears the message
    }

    @Test func textThatIsNotALinkIsRefusedBeforeAnyRequest() async {
        let h = hub()
        let m = IcsLinkViewModel(api: h)
        m.url = "example.com/a.ics"
        await m.add()
        #expect(h.calls.isEmpty && m.urlError == "Use an address that starts with https:// or webcal://.")
    }

    @Test func nonAdminTokenAndOfflineGoToTheBanner() async {
        let h = hub()
        h.icsResult = .failure(APIClientError.unauthorized(APIError(error: .init(code: "forbidden", message: "admin"))))
        let m = IcsLinkViewModel(api: h)
        m.url = "https://x.test/a.ics"
        await m.add()
        #expect(m.urlError == nil && m.banner == "This phone\u{2019}s token can\u{2019}t add accounts. Use a token with admin access.")
        h.icsResult = .failure(APIClientError.transport("x"))
        await m.add()
        #expect(m.banner == "Can\u{2019}t reach the server. Nothing was saved.")
    }

    @Test func emptyFieldCannotBeSubmitted() {
        let m = IcsLinkViewModel(api: hub())
        #expect(!m.canAdd)
        m.url = "   "
        #expect(!m.canAdd)
    }
}
