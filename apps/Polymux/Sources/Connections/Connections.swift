import SwiftUI
import PolymuxKit
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// Tells the server which device this is, and runs the connections the user allowed on it.
@MainActor @Observable
final class Connections {
    static let shared = Connections()

    #if os(iOS)
    static let available = ["location", "home", "alarms"]
    #else
    static let available = ["location"]
    #endif

    /// One server's connection to this device.
    private struct Link {
        let client: APIClient
        var listener: Task<Void, Never>?
    }

    /// The server on show, which the toggles apply to.
    var current = ""
    private(set) var issue: String?
    /// What the user allowed, by server; each server gets its own consent.
    private var grants: [String: Set<String>] = [:]
    private var links: [String: Link] = [:]
    private let defaults = UserDefaults.standard

    var enabled: Set<String> { grants[current] ?? [] }

    private init() {
        let center = NotificationCenter.default
        center.addObserver(forName: .NSSystemTimeZoneDidChange, object: nil, queue: .main) { _ in
            Task { @MainActor in Connections.shared.reportAll() }
        }
        #if os(iOS)
        // Streams die in the background; reconnect straight away on return.
        center.addObserver(forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main) { _ in
            Task { @MainActor in Connections.shared.restartAll() }
        }
        #endif
    }

    func connect(_ client: APIClient, address: String) {
        guard links[address]?.client.id != client.id else { return }
        links[address]?.listener?.cancel()
        links[address] = Link(client: client)
        if grants[address] == nil {
            // Grants were once shared by every server; they stay with the server in use when that changed.
            if let legacy = defaults.stringArray(forKey: "connections.enabled") {
                defaults.set(legacy, forKey: key(address))
                defaults.removeObject(forKey: "connections.enabled")
            }
            grants[address] = Set(defaults.stringArray(forKey: key(address)) ?? [])
        }
        restart(address)
    }

    func disconnect(_ address: String) {
        links.removeValue(forKey: address)?.listener?.cancel()
    }

    private func key(_ address: String) -> String { "connections.enabled.\(address)" }

    func allow(_ capability: String, _ on: Bool) async {
        if on {
            do { try await Self.authorize(capability) } catch {
                issue = error.localizedDescription
                return
            }
            grants[current, default: []].insert(capability)
        } else {
            grants[current, default: []].remove(capability)
        }
        defaults.set(enabled.sorted(), forKey: key(current))
        issue = nil
        report(current)
    }

    static func title(_ capability: String) -> String {
        switch capability {
        case "home": "Home"
        case "alarms": "Alarms"
        default: "Location"
        }
    }

    private func state(_ address: String) -> ClientState {
        #if os(iOS)
        let platform = ClientStatePlatform.ios
        #else
        let platform = ClientStatePlatform.macos
        #endif
        return ClientState(platform: platform, timezone: TimeZone.current.identifier,
                           capabilities: (grants[address] ?? []).filter(Self.available.contains).sorted())
    }

    private func report(_ address: String) {
        guard let client = links[address]?.client else { return }
        let state = state(address)
        Task { try? await client.reportClient(state) }
    }

    private func reportAll() {
        for address in links.keys { report(address) }
    }

    private func restartAll() {
        for address in links.keys { restart(address) }
    }

    private func restart(_ address: String) {
        links[address]?.listener?.cancel()
        guard let client = links[address]?.client else { return }
        links[address]?.listener = Task { [weak self] in
            var delay = 1.0
            while !Task.isCancelled {
                guard let state = self?.state(address) else { return }
                // Regular check-ins let the server notice when this device drops off the network.
                let checkIn = Task { [weak self] in
                    while !Task.isCancelled {
                        try? await Task.sleep(for: .seconds(10))
                        guard let state = self?.state(address) else { return }
                        try? await client.reportClient(state)
                    }
                }
                do {
                    try await client.reportClient(state)
                    for try await request in client.clientRequests() {
                        delay = 1
                        Task { await self?.run(request, client, address) }
                    }
                } catch {}
                checkIn.cancel()
                do { try await Task.sleep(for: .seconds(delay)) } catch { return }
                delay = min(delay * 2, 30)
            }
        }
    }

    private func run(_ request: ClientRequest, _ client: APIClient, _ address: String) async {
        var result = ClientResult()
        do {
            guard grants[address]?.contains(request.capability) == true else {
                throw ConnectionError("\(Self.title(request.capability)) is turned off on this device.")
            }
            let output: Data
            switch request.capability {
            case "location": output = try await Locator.shared.current()
            #if os(iOS)
            case "home": output = try await HomeBridge.shared.run(command(request))
            case "alarms": output = try await AlarmBridge.run(command(request))
            #endif
            default: throw ConnectionError("This device can't run \(request.capability).")
            }
            result.output = String(decoding: output, as: UTF8.self)
        } catch {
            result.error = error.localizedDescription
        }
        try? await client.answerClient(id: request.id, result: result)
    }

    private func command(_ request: ClientRequest) throws -> DeviceCommand {
        try JSONDecoder().decode(DeviceCommand.self, from: Data(request.input.utf8))
    }

    private static func authorize(_ capability: String) async throws {
        switch capability {
        case "location": try await Locator.shared.authorize()
        #if os(iOS)
        case "home": try await HomeBridge.shared.authorize()
        case "alarms": try await AlarmBridge.authorize()
        #endif
        default: throw ConnectionError("Not available on this device.")
        }
    }
}

/// Arguments the server's device tools send.
struct DeviceCommand: Decodable, Sendable {
    var action: String?
    var accessory: String?
    var characteristic: String?
    var value: Double?
    var scene: String?
    var time: String?
    var weekdays: [String]?
    var label: String?
    var id: String?
}

struct ConnectionError: LocalizedError {
    let errorDescription: String?
    init(_ message: String) { errorDescription = message }
}

/// Encodes a tool result for the server.
func encoded(_ value: some Encodable) throws -> Data {
    let encoder = JSONEncoder()
    encoder.outputFormatting = .sortedKeys
    return try encoder.encode(value)
}

/// Runs work with a time limit, so a stuck framework call can't hold a request.
func limited<T: Sendable>(_ seconds: Double, _ message: String, _ work: @escaping @Sendable () async throws -> T) async throws -> T {
    try await withThrowingTaskGroup(of: T.self) { group in
        group.addTask { try await work() }
        group.addTask {
            try await Task.sleep(for: .seconds(seconds))
            throw ConnectionError(message)
        }
        defer { group.cancelAll() }
        return try await group.next()!
    }
}
