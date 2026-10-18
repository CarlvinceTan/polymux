#if DEBUG
import Foundation
import SwiftUI
import PolymuxKit

// Only the explicit UI-test launch uses this isolated, deterministic transport.
final class VerifyFixture: URLProtocol, @unchecked Sendable {
    static var enabled: Bool { CommandLine.arguments.contains("--verify-fixture") }
    static var local: Bool { enabled && UserDefaults.standard.string(forKey: "PolymuxServer") == nil }
    // UI checks capture both themes: POLYMUX_VERIFY_THEME=light|dark forces the appearance.
    static var appearance: String {
        ProcessInfo.processInfo.environment["POLYMUX_VERIFY_APPEARANCE"]
            ?? ProcessInfo.processInfo.environment["POLYMUX_VERIFY_THEME"]
            ?? (CommandLine.arguments.contains("--verify-dark") ? "dark" : "light")
    }
    static let ids = ["t-chief", "t-aria", "t-milo", "t-nova", "t-long", "t-stream", "t-approval",
                      "grp-trip", "t-media", "t-activity", "t-fern", "grp-empty"]
    // Unread state matches the server fixture: t-aria 3, t-milo 1, t-nova 12, the rest read.
    static let unread: [String: Int64] = ["t-aria": 3, "t-milo": 1, "t-nova": 12]
    static let agents = ["Chief", "Aria", "Milo", "Nova", "Approval", "Fern"].map {
        Agent(id: "t-" + $0.lowercased(), name: $0, active: true)
    }
    static let threads = ids.map { id in
        let direct = agents.first { $0.id == id }
        let members: [String]
        switch id {
        case "t-long": members = ["t-chief", "t-aria"]
        case "t-stream", "grp-empty": members = ["t-nova", "t-fern"]
        case "grp-trip": members = ["t-chief", "t-aria", "t-milo"]
        case "t-media": members = ["t-aria", "t-milo"]
        case "t-activity": members = ["t-chief", "t-milo"]
        default: members = [id]
        }
        return PolymuxKit.Thread(id: id, agentID: members[0], unreadCount: unread[id] ?? 0,
                                 title: direct == nil ? id : "", agentIDs: members)
    }
    private static let answers = Answers()
    private static let sidebar = Sidebar()

    private static var toolEvents: [Event] {
        let date = "2026-10-14T12:00:00Z"
        let tools = [
            ToolActivity(id: "bash", runID: "tool-run", name: "bash", status: "completed",
                         summary: "go test ./...", output: "ok polymux 0.2s"),
            ToolActivity(id: "read-1", runID: "tool-run", name: "read", status: "completed", summary: "one.swift"),
            ToolActivity(id: "read-2", runID: "tool-run", name: "read", status: "completed", summary: "two.swift"),
            ToolActivity(id: "read-3", runID: "tool-run", name: "read", status: "completed", summary: "three.swift"),
            ToolActivity(id: "edit", runID: "tool-run", name: "edit", status: "completed", summary: "ChatPane.swift"),
            ToolActivity(id: "failed", runID: "tool-run", name: "bash", status: "failed",
                         summary: "false", output: "Command exited with status 1\n[redacted]")
        ]
        let run = Run(id: "tool-run", conversationID: "t-chief", trigger: "message", tools: tools,
                      attempt: 1, seq: 1, createdAt: date, updatedAt: date, activity: "generating",
                      agentID: "t-chief", status: "completed", summary: "Checked the project")
        let message = Message(runID: run.id, id: "tool-message", threadID: "t-chief", role: "assistant",
                              parts: [Part(kind: "text", text: "Checked the project and edited ChatPane.swift.")],
                              createdAt: date, status: "completed", seq: 2, agentID: "t-chief")
        var second = message
        second.id = "tool-second"
        second.seq = 3
        second.parts = [Part(kind: "text", text: "Second reply from the same turn.")]
        return [Event(seq: 1, conversationID: "t-chief", type: "run.done", createdAt: date, run: run),
                Event(seq: 2, conversationID: "t-chief", type: "message.done", message: message, createdAt: date),
                Event(seq: 3, conversationID: "t-chief", type: "message.done", message: second, createdAt: date)]
    }

    static func failAnswers() { answers.fail() }

    static func prompt(_ name: String) -> String { "\(name) keeps the workspace on track and reports back." }
    static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [VerifyFixture.self]
        return URLSession(configuration: configuration)
    }
    static let activity: [Event] = {
        let thread = "t-activity"
        let at = "2026-10-13T12:00:00Z"
        let message = Message(id: "fixture-request", threadID: thread, role: "user",
                              parts: [Part(kind: "text", text: "Review the release and remember the rollout plan.")],
                              createdAt: at, status: "done", seq: 1)
        let rows = [
            Activity(kind: "tool.called", verb: "Checked", objectLabel: "release status", iconOrAgentID: "terminal", at: at),
            Activity(kind: "agent.messaged", verb: "Messaged", at: at, agentIDs: ["t-chief", "t-milo"]),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "memory.created", verb: "Saved", objectID: "rollout-plan", objectLabel: "Rollout plan", at: at)
        ]
        return [Event(seq: 1, conversationID: thread, type: "message.done", message: message, createdAt: at)]
            + rows.enumerated().map { index, row in
                Event(seq: Int64(index + 2), conversationID: thread, type: "activity", agentID: "t-chief",
                      createdAt: at, payload: row)
            }
    }()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        if request.httpMethod == "PATCH", request.url?.path.contains("/threads/") == true {
            if CommandLine.arguments.contains("--verify-pin-offline") { fail(); return }
            do {
                let data = try Self.sidebar.patch(request)
                reply(data)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
            return
        }
        if request.url?.lastPathComponent == "stream",
           CommandLine.arguments.contains("--verify-tools") || CommandLine.arguments.contains("--verify-settings") {
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil,
                                           headerFields: ["Content-Type": "text/event-stream"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: Data(": connected\n\n".utf8))
            return
        }
        if request.url?.lastPathComponent == "answer" {
            Self.answers.add(self)
            return
        }
        do {
            let data: Data
            if let agent = Self.agents.first(where: { $0.id == request.url?.lastPathComponent }) {
                // The profile editors load an AgentProfile, not an empty object, in UI checks.
                data = try JSONEncoder().encode(AgentProfile(agent: agent, prompt: Self.prompt(agent.name)))
            } else {
                switch request.url?.lastPathComponent {
                case "agents": data = try JSONEncoder().encode(Self.agents)
                case "threads": data = try JSONEncoder().encode(Self.sidebar.threads)
                case "settings":
                    let theme = CommandLine.arguments.contains("--verify-tools")
                        ? (CommandLine.arguments.contains("--tools-dark") ? "dark" : "light") : Self.appearance
                    data = try JSONEncoder().encode(Settings(appearance: theme, sidebarWidth: 360))
                case "network":
                    if CommandLine.arguments.contains("--verify-network-fail") { fail(); return }
                    if CommandLine.arguments.contains("--verify-network-slow") { return }
                    let devices: [[String: Any]] = CommandLine.arguments.contains("--verify-network-empty") ? [] : [
                        ["id": "d-mac", "name": "Local Mac app"],
                        ["id": "d-ios", "name": "Carlvince’s iPhone"],
                    ]
                    data = try JSONSerialization.data(withJSONObject: [
                        "server": ["name": "Home server", "build": "0.4.0", "platform": "macOS", "role": "host"],
                        "devices": devices,
                    ])
                case "invite":
                    data = try JSONEncoder().encode(Invite(url: "http://192.168.1.20:7337", token: "fixture-token",
                                                           ticket: "endpoint" + String(repeating: "a", count: 140)))
                case "messages": data = try JSONEncoder().encode([Message]())
                case "events":
                    let after = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?
                        .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
                    let seeded: [Event]
                    if CommandLine.arguments.contains("--verify-tools"), request.url?.path.contains("/t-chief/") == true {
                        seeded = Self.toolEvents
                    } else {
                        seeded = request.url?.path.contains("/t-activity/") == true ? Self.activity : []
                    }
                    data = try JSONEncoder().encode(seeded.filter { $0.seq > after })
                default:
                    data = request.url?.path.contains("/activity/") == true
                        ? try JSONEncoder().encode(["text": "Rollout plan: verify the release before enabling the routine."])
                        : Data("{}".utf8)
                }
            }
            reply(data)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    private func reply(_ data: Data) {
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil,
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() { Self.answers.remove(self) }

    private func fail() {
        client?.urlProtocol(self, didFailWithError: URLError(.cannotConnectToHost))
    }

    private final class Sidebar: @unchecked Sendable {
        private let lock = NSLock()
        private var values = VerifyFixture.threads.map { thread in
            var thread = thread
            if CommandLine.arguments.contains("--verify-pins"), ["t-chief", "t-aria"].contains(thread.id) {
                thread.pinned = true
                thread.pinOrder = thread.id == "t-chief" ? 0 : 1
                thread.unreadCount = thread.id == "t-aria" ? 3 : 0
            }
            return thread
        }

        var threads: [PolymuxKit.Thread] { lock.withLock { values } }

        func patch(_ request: URLRequest) throws -> Data {
            var body = request.httpBody ?? Data()
            if let stream = request.httpBodyStream {
                stream.open()
                defer { stream.close() }
                var bytes = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&bytes, maxLength: bytes.count)
                    guard count > 0 else { break }
                    body.append(contentsOf: bytes.prefix(count))
                }
            }
            let patch = try JSONDecoder().decode(UpdateThreadRequest.self, from: body)
            return try lock.withLock {
                guard let index = values.firstIndex(where: { $0.id == request.url?.lastPathComponent }) else {
                    throw URLError(.resourceUnavailable)
                }
                if let pinned = patch.pinned {
                    values[index].pinned = pinned
                    values[index].pinOrder = pinned ? patch.pinOrder : nil
                }
                return try JSONEncoder().encode(values[index])
            }
        }
    }

    private final class Answers: @unchecked Sendable {
        private let lock = NSLock()
        private var pending: [VerifyFixture] = []
        private var failed = false

        func add(_ fixture: VerifyFixture) {
            let fail = lock.withLock {
                if failed { return true }
                pending.append(fixture)
                return false
            }
            if fail { fixture.fail() }
        }

        func remove(_ fixture: VerifyFixture) {
            lock.withLock { pending.removeAll { $0 === fixture } }
        }

        func fail() {
            let requests = lock.withLock {
                failed = true
                defer { pending.removeAll() }
                return pending
            }
            for request in requests { request.fail() }
        }
    }
}

struct ApprovalFixture: View {
    @Environment(\.colorScheme) private var scheme
    private let part = Part(kind: "focus", question: "What should we focus on today?", options: [
        ChoiceOption(id: "day", label: "Plan my day"),
        ChoiceOption(id: "code", label: "Build something")
    ])
    private let client = APIClient(baseURL: URL(string: "http://approval.invalid")!,
                                   session: VerifyFixture.session(), token: "")

    var body: some View {
        VStack(spacing: 16) {
            FocusCard(part: part, client: client, agentID: "t-approval", bubble: BubbleStyle.incoming(scheme))
            Button("Fail answer") { VerifyFixture.failAnswers() }
                .accessibilityValue(scheme == .dark ? "dark" : "light")
        }
            .frame(maxWidth: 340)
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .neutralBackground()
            .preferredColorScheme(CommandLine.arguments.contains("--approval-dark") ? .dark : .light)
    }
}
#endif
