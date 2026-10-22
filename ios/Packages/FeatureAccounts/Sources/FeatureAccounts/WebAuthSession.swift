import AuthenticationServices
import Foundation
import UIKit

public enum WebAuthError: Error, Equatable, Sendable {
    case cancelled
    case failed(String)
}

/// Seam over the system sign-in sheet so view models can be tested with a fake.
@MainActor
public protocol WebAuthSession {
    /// Opens `url`, returns the URL the page redirected to (scheme == `callbackScheme`).
    func authenticate(url: URL, callbackScheme: String) async throws -> URL
}

@MainActor
public final class SystemWebAuthSession: NSObject, WebAuthSession, ASWebAuthenticationPresentationContextProviding {
    private var session: ASWebAuthenticationSession?

    public override init() { super.init() }

    public func authenticate(url: URL, callbackScheme: String) async throws -> URL {
        try await withCheckedThrowingContinuation { cont in
            let s = ASWebAuthenticationSession(url: url, callbackURLScheme: callbackScheme) { callback, error in
                if let callback {
                    cont.resume(returning: callback)
                } else if let e = error as? ASWebAuthenticationSessionError, e.code == .canceledLogin {
                    cont.resume(throwing: WebAuthError.cancelled)
                } else {
                    cont.resume(throwing: WebAuthError.failed(error?.localizedDescription ?? "Sign in failed"))
                }
            }
            s.presentationContextProvider = self
            self.session = s
            if !s.start() { cont.resume(throwing: WebAuthError.failed("Could not open the sign-in page")) }
        }
    }

    public func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        if let w = scenes.flatMap(\.windows).first(where: \.isKeyWindow) { return w }
        return scenes.first.map { ASPresentationAnchor(windowScene: $0) } ?? ASPresentationAnchor()
    }
}
