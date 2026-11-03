import AuthenticationServices
import Network
import PolymuxKit
import SwiftUI

/// Carries a mailbox sign-in through the browser. The provider sends the browser back to a port on
/// this device, because the server may be another machine, and the server redeems what arrives.
@MainActor
final class HubSignIn: NSObject, ASWebAuthenticationPresentationContextProviding {
    private static let port: NWEndpoint.Port = 47666
    private static let page = "<!doctype html><meta charset=\"utf-8\"><title>Polymux</title>"
        + "<body style=\"font:15px -apple-system,sans-serif;display:grid;place-items:center;height:100vh;margin:0\"><p>You can close this window.</p></body>"

    private var listener: NWListener?
    private var session: ASWebAuthenticationSession?
    private var waiting: CheckedContinuation<HubMailCallback, Error>?

    static func run(_ url: URL) async throws -> HubMailCallback {
        #if DEBUG
        if VerifyFixture.enabled { return HubMailCallback(code: "fixture", state: "fixture", error: "") }
        #endif
        let flow = HubSignIn()
        return try await flow.begin(url)
    }

    private func begin(_ url: URL) async throws -> HubMailCallback {
        try await withCheckedThrowingContinuation { continuation in
            waiting = continuation
            do {
                try listen()
                open(url)
            } catch {
                finish(.failure(error))
            }
        }
    }

    private func listen() throws {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: Self.port)
        parameters.allowLocalEndpointReuse = true
        let listener = try NWListener(using: parameters)
        listener.newConnectionHandler = { [weak self] connection in
            connection.start(queue: .main)
            connection.receive(minimumIncompleteLength: 1, maximumLength: 16_384) { data, _, _, _ in
                let request = data.flatMap { String(data: $0, encoding: .utf8) } ?? ""
                MainActor.assumeIsolated { self?.answer(connection, request) }
            }
        }
        listener.start(queue: .main)
        self.listener = listener
    }

    private func answer(_ connection: NWConnection, _ request: String) {
        let target = request.split(separator: " ").dropFirst().first.map(String.init) ?? ""
        let address = URLComponents(string: target)
        let found = address?.path == "/mail/callback"
        let status = found ? "200 OK" : "404 Not Found"
        let body = found ? Self.page : ""
        let reply = "HTTP/1.1 \(status)\r\nContent-Type: text/html\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)"
        connection.send(content: Data(reply.utf8), completion: .contentProcessed { _ in connection.cancel() })
        guard found else { return }
        let value = { (name: String) in address?.queryItems?.first { $0.name == name }?.value ?? "" }
        finish(.success(HubMailCallback(code: value("code"), state: value("state"), error: value("error"))))
    }

    private func open(_ url: URL) {
        // The redirect lands on the loopback port, so this scheme never fires. Closing the page cancels.
        let session = ASWebAuthenticationSession(url: url, callback: .customScheme("polymux-mail")) { [weak self] _, _ in
            MainActor.assumeIsolated { self?.finish(.failure(CancellationError())) }
        }
        session.presentationContextProvider = self
        self.session = session
        session.start()
    }

    private func finish(_ result: Result<HubMailCallback, Error>) {
        guard let waiting else { return }
        self.waiting = nil
        listener?.cancel()
        session?.cancel()
        waiting.resume(with: result)
    }

    nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            #if os(macOS)
            NSApp.keyWindow ?? ASPresentationAnchor()
            #else
            UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first?.keyWindow ?? ASPresentationAnchor()
            #endif
        }
    }
}
