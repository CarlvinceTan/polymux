import Foundation
import Observation

@MainActor @Observable
public final class ProviderSignIn {
    public private(set) var state: ProviderLogin?
    public private(set) var issue: String?
    public private(set) var sending = false
    private let client: APIClient

    public init(client: APIClient) { self.client = client }

    public func connect(option: ProviderOption, provider: String? = nil) async {
        defer {
            if let state, !state.done {
                let client = client
                Task { try? await client.cancelLogin(id: state.id) }
            }
        }
        do {
            state = try await client.startLogin(type: option.type, auth: option.auth, provider: provider)
            while let current = state, !current.done, !Task.isCancelled {
                do {
                    state = try await client.providerLogin(id: current.id, after: current.revision)
                    issue = nil
                } catch {
                    if Task.isCancelled { return }
                    guard error.isTransient else { throw error }
                    issue = error.isOffline ? nil : "Sign-in is waiting for a connection."
                    try await Task.sleep(for: .seconds(2))
                }
            }
        } catch {
            if !Task.isCancelled { issue = error.reason(or: "Couldn’t start sign-in.") }
        }
    }

    public func submit(_ value: String) async {
        guard let current = state, let prompt = current.prompt, !sending else { return }
        sending = true
        state?.prompt = nil
        defer { sending = false }
        do {
            try await client.answerLogin(id: current.id, prompt: prompt.id, value: value)
            issue = nil
        } catch {
            if state?.revision == current.revision { state?.prompt = prompt }
            if !Task.isCancelled { issue = error.reason(or: "Couldn’t send this answer.") }
        }
    }
}
