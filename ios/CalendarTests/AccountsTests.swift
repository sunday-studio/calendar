import CalendarCore
import Foundation
import Testing
@testable import FeatureAccounts

// MARK: Fakes

final class FakeHub: HubAPI, @unchecked Sendable {
    var helloResult: Result<Hello, Error> = .success(Hello(server: "calendard", version: "0.1.0", apiVersion: 1, scope: "admin", tokenId: "t"))
    var connectResult: Result<ConnectSession, Error> = .success(ConnectSession(authUrl: "https://accounts.example/auth", state: "s"))
    var accountsQueue: [Account] = []   // successive replies for account(id:); last repeats
    var accountsList: [Account] = []
    var patchError: Error?
    var deleteError: Error?
    private(set) var calls: [String] = []
    private(set) var polls = 0

    func hello() async throws -> Hello { try helloResult.get() }
    func accounts() async throws -> [Account] { accountsList }
    func account(id: String) async throws -> Account {
        polls += 1
        return accountsQueue.count > 1 ? accountsQueue.removeFirst() : accountsQueue[0]
    }
    func calendars() async throws -> [HubCalendar] { [] }
    func connectAccount(provider: String, reauthAccountId: String?) async throws -> ConnectSession {
        calls.append("connect:\(provider)"); return try connectResult.get()
    }
    func reconnectAccount(id: String) async throws -> ConnectSession {
        calls.append("reconnect:\(id)"); return try connectResult.get()
    }
    func patchAccount(id: String, color: String?, displayName: String?) async throws -> Account {
        calls.append("patch:\(color ?? "")")
        if let patchError { throw patchError }
        var a = accountsList[0]; a.color = color ?? a.color; return a
    }
    func deleteAccount(id: String) async throws {
        calls.append("delete:\(id)")
        if let deleteError { throw deleteError }
    }
    var icsResult: Result<Account, Error> = .success(Account(id: "ics1", provider: "ics", email: "feeds.example.com", displayName: "Fixtures", color: "slate", status: "active"))
    func addIcsAccount(url: String, name: String?, color: String?) async throws -> Account {
        calls.append("ics:\(url)"); return try icsResult.get()
    }
}

@MainActor final class FakeAuth: WebAuthSession {
    var result: Result<URL, Error>
    private(set) var opened: [(URL, String)] = []
    init(_ result: Result<URL, Error>) { self.result = result }
    func authenticate(url: URL, callbackScheme: String) async throws -> URL {
        opened.append((url, callbackScheme)); return try result.get()
    }
}

func acct(_ status: String, detail: String? = nil, color: String = "cobalt") -> Account {
    Account(id: "A1", email: "me@example.com", displayName: "Me", color: color, status: status, statusDetail: detail)
}

// MARK: Pairing

@MainActor @Suite struct PairingTests {
    final class Box: @unchecked Sendable { var saved: [HubConfig] = []; var failSave = false }

    func make(_ hub: FakeHub, box: Box, host: String = "hub.tail1.ts.net", token: String = "cal_abc") -> PairingViewModel {
        PairingViewModel(host: host, token: token, makeAPI: { _ in hub }, save: {
            if box.failSave { throw KeychainError.status(-34018) }
            box.saved.append($0)
        }, stepDelay: .zero)
    }

    func state(_ m: PairingViewModel, _ k: CheckKind) -> CheckState { m.steps.first { $0.id == k }!.state }

    @Test func successSavesConfigAndShowsServer() async {
        let box = Box(); let m = make(FakeHub(), box: box)
        await m.submit()
        guard case .paired(let h) = m.step else { Issue.record("step \(m.step)"); return }
        #expect(h.server == "calendard")
        #expect(box.saved == [HubConfig(baseURL: URL(string: "https://hub.tail1.ts.net")!, token: "cal_abc")])
        #expect(m.steps.allSatisfy { if case .ok = $0.state { true } else { false } })
    }

    @Test func unreachableFailsFirstStepAndDoesNotSave() async {
        let hub = FakeHub(); hub.helloResult = .failure(APIClientError.transport("timed out"))
        let box = Box(); let m = make(hub, box: box)
        await m.submit()
        guard case .failed(let f) = m.step else { Issue.record("step \(m.step)"); return }
        #expect(f.headline.contains("hub.tail1.ts.net"))
        #expect(f.hints.count >= 2)
        #expect(state(m, .reach) == .failed("timed out"))
        #expect(state(m, .token) == .pending)
        #expect(f.detail.hasPrefix("Nothing was saved. Tried https://hub.tail1.ts.net."))
        #expect(f.detail.hasSuffix("timed out"))
        #expect(box.saved.isEmpty)
    }

    @Test func tlsFailureFailsTheReachStep() async {
        let hub = FakeHub(); hub.helloResult = .failure(APIClientError.tls("bad cert"))
        let box = Box(); let m = make(hub, box: box)
        await m.submit()
        #expect(state(m, .reach) == .failed("bad cert"))
        #expect(state(m, .token) == .pending)
        #expect(box.saved.isEmpty)
    }

    @Test func rejectedTokenStopsAtSecondStep() async {
        let hub = FakeHub(); hub.helloResult = .failure(APIClientError.unauthorized(nil))
        let box = Box(); let m = make(hub, box: box)
        await m.submit()
        #expect(state(m, .reach) == .ok(nil))
        if case .failed = state(m, .token) {} else { Issue.record("token step should fail") }
        #expect(state(m, .version) == .pending)
        #expect(box.saved.isEmpty)
    }

    @Test func versionMismatchFailsLastStepAndDoesNotSave() async {
        let hub = FakeHub(); hub.helloResult = .success(Hello(server: "calendard", version: "9.0", apiVersion: 2, scope: "admin", tokenId: "t"))
        let box = Box(); let m = make(hub, box: box)
        await m.submit()
        if case .failed = state(m, .version) {} else { Issue.record("version step should fail") }
        guard case .failed = m.step else { Issue.record("step \(m.step)"); return }
        #expect(box.saved.isEmpty)
    }

    @Test func keychainFailureIsAFailureNotASilentSuccess() async {
        let box = Box(); box.failSave = true
        let m = make(FakeHub(), box: box)
        await m.submit()
        guard case .failed(let f) = m.step else { Issue.record("step \(m.step)"); return }
        #expect(f.headline.contains("save the token on this phone"))
        #expect(f.isLocal && f.hints.isEmpty)           // not blamed on the server
        if case .failed = state(m, .save) {} else { Issue.record("save step should fail") }
        #expect(state(m, .version) == .ok("0.1.0"))
        #expect(m.pairedConfig == nil)
    }

    @Test func retryAfterFailureCanSucceed() async {
        let hub = FakeHub(); hub.helloResult = .failure(APIClientError.transport("x"))
        let box = Box(); let m = make(hub, box: box)
        await m.submit()
        hub.helloResult = .success(Hello(server: "calendard", version: "0.1.0", apiVersion: 1, scope: "admin", tokenId: "t"))
        await m.submit()
        if case .paired = m.step {} else { Issue.record("step \(m.step)") }
        #expect(box.saved.count == 1)
    }

    @Test func badInputNeverHitsTheNetwork() async {
        let box = Box()
        let m = make(FakeHub(), box: box, host: "not a host", token: "cal_x")
        await m.submit()
        #expect(m.step == .form || m.step == .welcome)
        #expect(m.formError != nil)
        m.host = "hub.ts.net"; m.token = "oops"
        await m.submit()
        #expect(m.formError?.contains("cal_") == true)
        #expect(box.saved.isEmpty)
    }

    @Test func editAddressReturnsToForm() async {
        let hub = FakeHub(); hub.helloResult = .failure(APIClientError.transport("x"))
        let m = make(hub, box: Box())
        await m.submit()
        m.editAddress()
        #expect(m.step == .form)
        #expect(m.host == "hub.tail1.ts.net") // kept for editing
    }

    @Test func normalizeAddresses() {
        #expect(PairingViewModel.normalize("hub.ts.net")?.absoluteString == "https://hub.ts.net")
        #expect(PairingViewModel.normalize(" https://hub.ts.net/ ")?.absoluteString == "https://hub.ts.net")
        #expect(PairingViewModel.normalize("localhost:8080")?.absoluteString == "http://localhost:8080")
        #expect(PairingViewModel.normalize("127.0.0.1:8080")?.absoluteString == "http://127.0.0.1:8080")
        #expect(PairingViewModel.normalize("100.127.10.66:8790")?.absoluteString == "http://100.127.10.66:8790")
        #expect(PairingViewModel.normalize("192.168.1.20:8787")?.absoluteString == "http://192.168.1.20:8787")
        #expect(PairingViewModel.normalize("10.0.0.5")?.absoluteString == "http://10.0.0.5")
        #expect(PairingViewModel.normalize("mini.local:8787")?.absoluteString == "http://mini.local:8787")
        #expect(PairingViewModel.normalize("8.8.8.8")?.absoluteString == "https://8.8.8.8")
        #expect(PairingViewModel.normalize("100.128.0.1")?.absoluteString == "https://100.128.0.1")
        #expect(PairingViewModel.normalize("hub.tail1234.ts.net")?.absoluteString == "https://hub.tail1234.ts.net")
        #expect(PairingViewModel.normalize("") == nil)
        #expect(PairingViewModel.normalize("a b") == nil)
        #expect(PairingViewModel.normalize("ftp://x.y") == nil)
    }

    @Test func deepLinkPrefillsForm() {
        let m = PairingViewModel(stepDelay: .zero)
        #expect(m.step == .welcome)
        m.apply(DeepLink.parse(URL(string: "calendar://pair?host=hub.ts.net&token=cal_zzz")!)!)
        #expect(m.step == .form)
        #expect(m.host == "hub.ts.net")
        #expect(m.token == "cal_zzz")
        #expect(PairingViewModel(host: "h", token: nil, stepDelay: .zero).step == .form)
    }

    @Test func pasteRecognisesLinkTokenAndAddress() {
        let m = PairingViewModel(stepDelay: .zero)
        m.paste("cal_secret")
        #expect(m.token == "cal_secret")
        m.paste("hub.ts.net")
        #expect(m.host == "hub.ts.net")
        m.paste("calendar://pair?host=other.ts.net&token=cal_new")
        #expect(m.host == "other.ts.net" && m.token == "cal_new")
        m.paste("   ")
        #expect(m.host == "other.ts.net")
    }
}

// MARK: Deep links

struct DeepLinkTests {
    @Test func parses() {
        #expect(DeepLink.parse(URL(string: "calendar://connected?account=A1&status=connecting")!) == .connected(accountId: "A1", status: "connecting"))
        #expect(DeepLink.parse(URL(string: "calendar://connected?error=access_denied")!) == .connectError(code: "access_denied"))
        #expect(DeepLink.parse(URL(string: "calendar://connected")!) == .connectError(code: "internal"))
        #expect(DeepLink.parse(URL(string: "https://x/y")!) == nil)
        #expect(DeepLink.parse(URL(string: "calendar://other")!) == nil)
    }
}

// MARK: Connect

@MainActor @Suite struct ConnectTests {
    let ok = URL(string: "calendar://connected?account=A1&status=connecting")!

    func vm(_ kind: ConnectKind = .add(provider: "google"), hub: FakeHub, auth: FakeAuth, polls: Int = 5) -> ConnectViewModel {
        ConnectViewModel(kind: kind, api: hub, auth: auth, pollInterval: .zero, maxPolls: polls)
    }

    @Test func addFlowPollsUntilNotSyncing() async {
        let hub = FakeHub(); hub.accountsQueue = [acct("syncing"), acct("syncing"), acct("active")]
        let auth = FakeAuth(.success(ok))
        let m = vm(hub: hub, auth: auth)
        await m.run()
        guard case .connected(let a) = m.phase else { Issue.record("phase \(m.phase)"); return }
        #expect(a.statusKind == .active)
        #expect(hub.polls == 3)
        #expect(hub.calls == ["connect:google"])
        #expect(auth.opened.first?.0.absoluteString == "https://accounts.example/auth")
        #expect(auth.opened.first?.1 == "calendar")
    }

    @Test func reconnectUsesReconnectEndpoint() async {
        let hub = FakeHub(); hub.accountsQueue = [acct("active")]
        let m = vm(.reconnect(accountId: "A1"), hub: hub, auth: FakeAuth(.success(ok)))
        await m.run()
        #expect(hub.calls == ["reconnect:A1"])
        if case .connected = m.phase {} else { Issue.record("phase \(m.phase)") }
    }

    @Test func errorCallbackShowsPlainMessage() async {
        let hub = FakeHub()
        let auth = FakeAuth(.success(URL(string: "calendar://connected?error=identity_mismatch")!))
        let m = vm(.reconnect(accountId: "A1"), hub: hub, auth: auth)
        await m.run()
        guard case .failed(let f) = m.phase else { Issue.record("phase \(m.phase)"); return }
        #expect(f.title == "Different Google account")
        #expect(hub.polls == 0)
    }

    @Test func everyServerErrorCodeHasItsOwnWords() {
        let titles = ["access_denied", "invalid_state", "identity_mismatch", "no_refresh_token", "exchange_failed", "internal"]
            .map { ConnectViewModel.describe(code: $0, reconnect: false).title }
        #expect(Set(titles).count == titles.count)
    }

    @Test func userCancellingTheSheetIsNotAnError() async {
        let m = vm(hub: FakeHub(), auth: FakeAuth(.failure(WebAuthError.cancelled)))
        await m.run()
        #expect(m.phase == .cancelled)
    }

    @Test func sheetFailureIsReportedAndRetryable() async {
        let auth = FakeAuth(.failure(WebAuthError.failed("boom")))
        let hub = FakeHub(); hub.accountsQueue = [acct("active")]
        let m = vm(hub: hub, auth: auth)
        await m.run()
        if case .failed = m.phase {} else { Issue.record("phase \(m.phase)") }
        auth.result = .success(ok)
        await m.run()
        if case .connected = m.phase {} else { Issue.record("phase \(m.phase)") }
    }

    @Test func serverWithoutGoogleExplainsItself() async {
        let hub = FakeHub()
        hub.connectResult = .failure(APIClientError.server(status: 503, try! JSONDecoder().decode(APIError.self, from: Data(#"{"error":{"code":"unavailable","message":"Google is not configured"}}"#.utf8))))
        let auth = FakeAuth(.success(ok))
        let m = vm(hub: hub, auth: auth)
        await m.run()
        guard case .failed(let f) = m.phase else { Issue.record("phase \(m.phase)"); return }
        #expect(f.title.contains("Google"))
        #expect(auth.opened.isEmpty)
    }

    @Test func needsReauthAfterPollingIsAFailure() async {
        let hub = FakeHub(); hub.accountsQueue = [acct("needs_reauth", detail: "Google revoked access")]
        let m = vm(hub: hub, auth: FakeAuth(.success(ok)))
        await m.run()
        guard case .failed(let f) = m.phase else { Issue.record("phase \(m.phase)"); return }
        #expect(f.message == "Google revoked access")
    }

    @Test func stillSyncingAfterTimeoutCountsAsConnected() async {
        let hub = FakeHub(); hub.accountsQueue = [acct("syncing")]
        let m = vm(hub: hub, auth: FakeAuth(.success(ok)), polls: 3)
        await m.run()
        guard case .connected(let a) = m.phase else { Issue.record("phase \(m.phase)"); return }
        #expect(a.statusKind == .syncing)
        #expect(hub.polls == 3)
    }
}

// MARK: Accounts view models

@MainActor @Suite struct AccountsVMTests {
    @Test func listFlagsAccountsNeedingSignIn() async {
        let hub = FakeHub(); hub.accountsList = [acct("active"), Account(id: "B", email: "b@x", displayName: "B", color: "rose", status: "needs_reauth")]
        let m = AccountsListViewModel(api: hub)
        await m.load()
        #expect(m.phase == .loaded)
        #expect(m.needingSignIn.map(\.id) == ["B"])
    }

    @Test func colourChangeRevertsWhenServerRefuses() async {
        let hub = FakeHub(); hub.accountsList = [acct("active")]; hub.patchError = APIClientError.server(status: 500, nil)
        let m = AccountDetailViewModel(account: acct("active"), api: hub)
        await m.setColor("moss")
        #expect(m.account.color == "cobalt")
        #expect(m.errorMessage != nil)
        hub.patchError = nil
        await m.setColor("moss")
        #expect(m.account.color == "moss")
    }

    @Test func removeDeletesAndFlags() async {
        let hub = FakeHub()
        let m = AccountDetailViewModel(account: acct("active"), api: hub)
        await m.remove()
        #expect(m.removed)
        #expect(hub.calls == ["delete:A1"])
    }

    @Test func removeFailureKeepsAccount() async {
        let hub = FakeHub(); hub.deleteError = APIClientError.transport("down")
        let m = AccountDetailViewModel(account: acct("active"), api: hub)
        await m.remove()
        #expect(!m.removed)
        #expect(m.errorMessage != nil)
    }

    @Test func reconnectedShowsToast() async {
        let hub = FakeHub(); hub.accountsQueue = [acct("active")]
        let m = AccountDetailViewModel(account: acct("needs_reauth"), api: hub)
        await m.reconnected()
        #expect(m.toast == "Reconnected")
        #expect(m.account.statusKind == .active)
    }

    @Test func statusWordsCoverWireEnum() {
        let words = ["active", "syncing", "paused", "needs_reauth", "error"].map { StatusWords.word(acct($0)) }
        #expect(words == ["Up to date", "Syncing", "Paused", "Sign in needed", "Problem"])
    }
}
