#if DEBUG
import Foundation
import PolymuxKit

final class ProviderFixture: @unchecked Sendable {
    private let lock = NSLock()
    private var phase: Int64 = 0
    private var providers: [Provider] = []
    private var settings = Settings(appearance: VerifyFixture.appearance)

    func response(_ request: URLRequest) throws -> Data {
        try lock.withLock {
            let path = request.url!.path
            if path == "/settings" {
                if request.httpMethod == "PATCH" {
                    let patch = try JSONDecoder().decode(SettingsPatch.self, from: body(request))
                    if let value = patch.appearance { settings.appearance = value }
                    if let value = patch.switcherBadges { settings.switcherBadges = value }
                    if let value = patch.defaultModel { settings.defaultModel = value }
                    if let value = patch.defaultProvider { settings.defaultProvider = value }
                    settings.revision += 1
                }
                return try JSONEncoder().encode(settings)
            }
            if path == "/providers/options" {
                return try JSONEncoder().encode([ProviderOption(type: "fixture", name: "Subscription", auth: "oauth", label: "Subscription sign-in", subscription: true)])
            }
            if path == "/providers" { return try JSONEncoder().encode(providers) }
            if request.httpMethod == "DELETE" {
                phase = 0
                return Data()
            }
            if path.hasSuffix("/answer") {
                phase += 1
                if phase == 3 {
                    providers = [Provider(id: "subscription", type: "fixture", name: "Subscription", status: "oauth", enabled: true,
                                          models: [ProviderModel(id: "subscription-chat", tags: ["text"], enabled: true)])]
                }
                return Data()
            }
            if path == "/providers/logins", request.httpMethod == "POST" { phase = 1 }
            let prompt: LoginPrompt? = switch phase {
            case 1: LoginPrompt(id: 1, type: "manual_code", message: "Paste the redirect URL.", placeholder: "http://localhost/callback")
            case 2: LoginPrompt(id: 2, type: "select", message: "Choose an account.", options: [LoginChoice(id: "personal", label: "Personal")])
            default: nil
            }
            let events = [LoginEvent(type: "auth_url", url: "https://example.invalid/sign-in", instructions: "Finish sign-in in your browser."),
                          LoginEvent(type: "device_code", userCode: "TEST-1234", verificationUri: "https://example.invalid/device")]
            return try JSONEncoder().encode(ProviderLogin(id: "fixture", revision: phase, events: events, prompt: prompt,
                                                          done: phase == 3, provider: providers.first))
        }
    }

    private func body(_ request: URLRequest) -> Data {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return Data() }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var bytes = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&bytes, maxLength: bytes.count)
            guard count > 0 else { break }
            data.append(contentsOf: bytes.prefix(count))
        }
        return data
    }
}
#endif
