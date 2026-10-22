import CalendarCore
import Foundation
import Observation

public enum CheckKind: String, CaseIterable, Sendable { case reach, token, version, save }

public enum CheckState: Equatable, Sendable {
    case pending, running
    case ok(String?)
    case failed(String?)
}

public struct CheckStep: Identifiable, Equatable, Sendable {
    public var id: CheckKind
    public var title: String
    public var state: CheckState
}

public struct PairHint: Equatable, Sendable {
    public var title: String
    public var detail: String
}

public struct PairFailure: Equatable, Sendable {
    public var headline: String
    public var detail: String
    public var hints: [PairHint]
    /// The server was fine; this phone could not store the pairing (Keychain). Retry only.
    public var isLocal: Bool = false
}

public enum PairingStep: Equatable, Sendable {
    case welcome, form, checking
    case failed(PairFailure)
    case paired(Hello)
}

@MainActor
@Observable
public final class PairingViewModel {
    public static let supportedAPIVersion = 1

    public var host: String
    public var token: String
    public private(set) var step: PairingStep
    public private(set) var steps: [CheckStep] = PairingViewModel.freshSteps()
    public private(set) var formError: String?
    public private(set) var pairedConfig: HubConfig?

    @ObservationIgnored private let makeAPI: @Sendable (HubConfig) -> any HubAPI
    @ObservationIgnored private let save: (HubConfig) throws -> Void
    @ObservationIgnored private let stepDelay: Duration
    @ObservationIgnored private var attempt = 0

    public init(
        host: String? = nil, token: String? = nil,
        makeAPI: @escaping @Sendable (HubConfig) -> any HubAPI = { APIClient(config: $0) },
        save: @escaping (HubConfig) throws -> Void = { try HubConfigStore().save($0) },
        stepDelay: Duration = .milliseconds(350)
    ) {
        self.host = host ?? ""
        self.token = token ?? ""
        self.makeAPI = makeAPI
        self.save = save
        self.stepDelay = stepDelay
        // A deep link with any prefill skips the welcome screen.
        self.step = (host != nil || token != nil) ? .form : .welcome
    }

    static func freshSteps() -> [CheckStep] {
        [
            CheckStep(id: .reach, title: "Reach the server", state: .pending),
            CheckStep(id: .token, title: "Check the token", state: .pending),
            CheckStep(id: .version, title: "Read the server version", state: .pending),
            CheckStep(id: .save, title: "Save this phone", state: .pending),
        ]
    }

    // MARK: Input

    public func begin() { step = .form }

    /// Form -> welcome.
    public func back() { attempt += 1; formError = nil; step = .welcome }

    /// Checking -> form. A check in flight is abandoned and nothing is saved.
    public func cancel() { attempt += 1; step = .form }

    public func apply(_ link: DeepLink) {
        guard case .pair(let h, let t) = link else { return }
        if let h { host = h }
        if let t { token = t }
        formError = nil
        step = .form
    }

    /// Paste support: accepts a pairing link, a token, or an address.
    public func paste(_ text: String?) {
        let s = (text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !s.isEmpty else { return }
        if let url = URL(string: s), let link = DeepLink.parse(url) { apply(link); return }
        if s.hasPrefix("cal_") { token = s } else { host = s }
        formError = nil
    }

    public func editAddress() {
        formError = nil
        step = .form
    }

    /// `https://` unless the address is a local development host. Returns nil when unusable.
    public static func normalize(_ raw: String) -> URL? {
        var s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        while s.hasSuffix("/") { s.removeLast() }
        guard !s.isEmpty, !s.contains(" ") else { return nil }
        if !s.contains("://") {
            let h = s.lowercased()
            let local = h.hasPrefix("localhost") || h.hasPrefix("127.") || h.hasPrefix("[::1]")
            s = (local ? "http://" : "https://") + s
        }
        guard let url = URL(string: s), let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https", let h = url.host, !h.isEmpty else { return nil }
        return url
    }

    // MARK: Check

    public func submit() async {
        formError = nil
        guard let url = Self.normalize(host) else {
            formError = "That doesn’t look like an address. Try something like hub.tail1234.ts.net."
            return
        }
        let tok = token.trimmingCharacters(in: .whitespacesAndNewlines)
        guard tok.hasPrefix("cal_") else {
            formError = "Access tokens start with cal_. Make one on the server under API tokens."
            return
        }
        let config = HubConfig(baseURL: url, token: tok)
        attempt += 1
        let mine = attempt
        steps = Self.freshSteps()
        step = .checking
        let plain = url.scheme?.lowercased() == "http"
        let hostName = url.host ?? host

        mark(.reach, .running)
        await pause()
        do {
            let hello = try await makeAPI(config).hello()
            guard attempt == mine else { return }
            mark(.reach, .ok(plain ? "Plain http, local address" : nil)); await pause()
            guard attempt == mine else { return }
            mark(.token, .running); await pause()
            guard attempt == mine else { return }
            mark(.token, .ok(nil)); await pause()
            mark(.version, .running); await pause()
            guard attempt == mine else { return }
            guard hello.apiVersion == Self.supportedAPIVersion else {
                mark(.version, .failed("Server speaks v\(hello.apiVersion), app speaks v\(Self.supportedAPIVersion)"))
                step = .failed(PairFailure(
                    headline: "This app and the server don\u{2019}t match.",
                    detail: "\(hello.server) \(hello.version) uses API v\(hello.apiVersion). Nothing was saved.",
                    hints: [PairHint(title: "Update the older one",
                                     detail: hello.apiVersion > Self.supportedAPIVersion
                                        ? "Install the latest version of the app." : "Update the hub on your home server.")]))
                return
            }
            mark(.version, .ok(hello.version)); await pause()
            guard attempt == mine else { return }
            mark(.save, .running)
            do { try save(config) } catch {
                mark(.save, .failed(nil))
                step = .failed(PairFailure(
                    headline: "Couldn\u{2019}t save the token on this phone.",
                    detail: "The server is fine. Nothing was saved.", hints: [], isLocal: true))
                return
            }
            mark(.save, .ok(nil))
            pairedConfig = config
            await pause()
            guard attempt == mine else { return }
            step = .paired(hello)
        } catch let e as APIClientError {
            guard attempt == mine else { return }
            fail(e, host: hostName)
        } catch {
            guard attempt == mine else { return }
            fail(.transport(error.localizedDescription), host: hostName)
        }
    }

    private func fail(_ e: APIClientError, host: String) {
        switch e {
        case .transport(let msg):
            mark(.reach, .failed(msg))
            step = .failed(PairFailure(
                headline: "Can\u{2019}t reach \(host).", detail: "Nothing was saved.",
                hints: [
                    PairHint(title: "Tailscale is on", detail: "Open Tailscale and make sure this phone shows as connected."),
                    PairHint(title: "The server is running", detail: "The hub container should be up on your home server."),
                    PairHint(title: "The address is right", detail: "It should end in .ts.net and start with https."),
                ]))
        case .tls(let msg):
            mark(.reach, .failed(msg))
            step = .failed(PairFailure(
                headline: "The secure connection to \(host) failed.", detail: "Nothing was saved.",
                hints: [
                    PairHint(title: "The address starts with https", detail: "Plain http only works on this phone itself."),
                    PairHint(title: "HTTPS is set up on the server", detail: "Run tailscale serve on the home server."),
                ]))
        case .unauthorized:
            mark(.reach, .ok(nil)); mark(.token, .failed("The server refused the token"))
            step = .failed(PairFailure(
                headline: "The server didn\u{2019}t accept that token.", detail: "Nothing was saved.",
                hints: [
                    PairHint(title: "The token is copied in full", detail: "It starts with cal_ and has no spaces."),
                    PairHint(title: "The token is still valid", detail: "Make a new one on the server under API tokens."),
                ]))
        default:
            mark(.reach, .ok(nil)); mark(.token, .ok(nil))
            mark(.version, .failed("Unexpected reply"))
            step = .failed(PairFailure(
                headline: "That doesn\u{2019}t look like a Calendar server.", detail: "Nothing was saved.",
                hints: [PairHint(title: "This is the hub\u{2019}s address", detail: "Another service may be using this port.")]))
        }
    }

    private func mark(_ kind: CheckKind, _ state: CheckState) {
        if let i = steps.firstIndex(where: { $0.id == kind }) { steps[i].state = state }
    }

    private func pause() async {
        if stepDelay > .zero { try? await Task.sleep(for: stepDelay) }
    }
}
