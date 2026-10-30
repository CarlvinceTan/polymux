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
    static let agents = ["Chief", "Aria", "Milo", "Nova", "Approval", "Fern"].map { name in
        let avatar = CommandLine.arguments.contains("--verify-blocked") && name == "Chief"
            ? AvatarDesign(shape: "hexagone", color: "orange", expression: "neutre", followCursor: false,
                           animates: !CommandLine.arguments.contains("--verify-still")) : nil
        return Agent(id: "t-" + name.lowercased(), name: name, active: true, avatar: avatar)
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
        var thread = PolymuxKit.Thread(id: id, agentID: members[0], unreadCount: unread[id] ?? 0,
                                       title: direct == nil ? id : "", agentIDs: members)
        if CommandLine.arguments.contains("--verify-blocked"), ["t-chief", "t-aria"].contains(id) {
            thread.pinned = id == "t-chief"
            thread.pinOrder = id == "t-chief" ? 0 : nil
            var run = Run(id: "blocked-" + id, conversationID: id, trigger: RunTrigger.message,
                          seq: 1, createdAt: "2026-10-23T12:00:00Z", updatedAt: "2026-10-23T12:00:00Z", agentID: id,
                          status: id == "t-chief" ? RunStatus.waitingInput : RunStatus.completed, summary: "Waiting for your input")
            run.outcome = id == "t-aria" ? "waiting_input" : ""
            thread.runs = [run]
            thread.lastSeq = 1
        }
        if CommandLine.arguments.contains("--verify-reply-error"), id == "t-chief" {
            thread.runs = replyEvents.compactMap(\.run)
            thread.lastSeq = replyEvents.last?.seq ?? 0
        }
        if CommandLine.arguments.contains("--verify-markers") {
            if ["t-milo", "grp-empty"].contains(id) {
                thread.pinned = true
                thread.pinOrder = id == "t-milo" ? 1 : 2
                thread.unreadCount = 2
            }
            if id == "grp-empty" { thread.title = "Group" }
            if id == "grp-trip" || id == "t-approval" {
                let waiting = id == "grp-trip"
                let run = Run(id: "marker-" + id, conversationID: id, trigger: RunTrigger.message, seq: 1,
                              createdAt: "2026-10-23T12:00:00Z", updatedAt: "2026-10-23T12:00:00Z",
                              activity: waiting ? "" : RunActivity.generating, agentID: waiting ? "t-chief" : id,
                              status: waiting ? RunStatus.waitingInput : RunStatus.running,
                              summary: waiting ? "Waiting for your input" : "Working")
                thread.runs = [run]
                thread.lastSeq = 1
                thread.unreadCount = waiting ? 3 : 0
            }
        }
        return thread
    }
    private static let answers = Answers()
    private static let sidebar = Sidebar()
    private static let providers = ProviderFixture()
    private static let profiles = Profiles()
    private static let profileLoads = ProfileLoads()
    private let responseLock = NSLock()
    private var responseTask: Task<Void, Never>?

    /// 150 finished messages, more than one page, so the chat opens on the newest and pages back.
    private static let history: [Event] = (Int64(1)...150).map { seq in
        let date = WireDate.string(Date(timeIntervalSince1970: 1_790_000_000 + Double(seq) * 60))
        let message = Message(id: "history-\(seq)", threadID: "t-long", role: seq.isMultiple(of: 2) ? MessageRole.assistant : MessageRole.user,
                              parts: [Part(kind: PartKind.text, text: "History message \(seq)")],
                              createdAt: date, status: MessageStatus.done, seq: seq, agentID: seq.isMultiple(of: 2) ? "t-chief" : nil)
        return Event(seq: seq, conversationID: "t-long", type: EventType.messageDone, message: message, createdAt: date)
    }

    /// Three group members working at once (one fetching a page, two thinking), or Chief alone fetching.
    private static func busyEvents(_ conversation: String) -> [Event] {
        let date = WireDate.string(Date())
        let actions = ["t-chief": "web_search", "t-aria": "read_file", "t-milo": "bash", "t-nova": "apply_patch"]
        let members: [(String, String?)]
        if CommandLine.arguments.contains("--verify-actions"), let tool = actions[conversation] {
            members = [(conversation, tool)]
        } else {
            members = conversation == "t-chief"
                ? [("t-chief", CommandLine.arguments.contains("--verify-thinking") ? nil : "http_request")]
                : [("t-chief", "http_request"), ("t-aria", nil), ("t-milo", nil)]
        }
        return members.enumerated().map { index, member in
            let id = "busy-\(conversation)-\(index)"
            let tools = member.1.map { [ToolActivity(id: id, runID: id, name: $0, status: ToolActivityStatus.running)] }
            var run = Run(id: id, conversationID: conversation, trigger: RunTrigger.message, tools: tools,
                          attempt: 1, seq: Int64(index + 1), createdAt: date, updatedAt: date,
                          activity: tools == nil ? RunActivity.generating : RunActivity.tool,
                          agentID: member.0, status: RunStatus.running, summary: "Working")
            if CommandLine.arguments.contains("--verify-status-text") {
                run.statusText = member.1 == nil ? "Drafting a greeting" : "Searching for Artemis II dates"
            }
            return Event(seq: Int64(index + 1), conversationID: conversation, type: EventType.runUpdate, createdAt: date, run: run)
        }
    }

    /// A finished reply followed by one that is still streaming, for the sidebar preview.
    private static var streamPreviewEvents: [Event] {
        let date = WireDate.string(Date())
        let older = Message(id: "stream-older", threadID: "t-chief", role: MessageRole.assistant,
                            parts: [Part(kind: PartKind.text, text: "Earlier reply")],
                            createdAt: date, status: MessageStatus.done, seq: 1, agentID: "t-chief")
        let streaming = Message(id: "stream-current", threadID: "t-chief", role: MessageRole.assistant,
                                parts: [Part(kind: PartKind.text, text: "Half-written reply")],
                                createdAt: date, status: MessageStatus.streaming, seq: 2, agentID: "t-chief")
        let run = Run(id: "stream-preview", conversationID: "t-chief", trigger: RunTrigger.message,
                      attempt: 1, seq: 3, createdAt: date, updatedAt: date, activity: RunActivity.generating,
                      agentID: "t-chief", status: RunStatus.running, summary: "Composing reply")
        return [
            Event(seq: 1, conversationID: "t-chief", type: EventType.messageDone, message: older, createdAt: date),
            Event(seq: 2, conversationID: "t-chief", type: EventType.messageStart, message: streaming, createdAt: date),
            Event(seq: 3, conversationID: "t-chief", type: EventType.runStart, createdAt: date, run: run),
        ]
    }

    private static var replyEvents: [Event] {
        let messages = [
            Message(id: "partial-reply", threadID: "t-chief", role: MessageRole.assistant,
                    parts: [Part(kind: PartKind.text, text: "Server-authored reply.")],
                    createdAt: "2026-10-27T06:00:00Z", status: MessageStatus.error, seq: 1, agentID: "t-chief"),
            Message(runID: "failed-reply", error: WireError(detail: "Private provider diagnostics", code: "provider", message: "A server-reported problem."),
                    id: "empty-reply", threadID: "t-chief", role: MessageRole.assistant,
                    createdAt: "2026-10-27T06:01:00Z", status: MessageStatus.error, seq: 2, agentID: "t-chief")
        ]
        var events = messages.map {
            Event(seq: $0.seq, conversationID: $0.threadID, type: EventType.messageDone, message: $0, createdAt: $0.createdAt)
        }
        let failed = Run(id: "failed-reply", conversationID: "t-chief", trigger: RunTrigger.message,
                         seq: 3, createdAt: "2026-10-27T06:01:00Z", error: messages.last?.error,
                         agentID: "t-chief", status: RunStatus.failed, summary: "Reply failed")
        events.append(Event(seq: 3, conversationID: "t-chief", type: EventType.runDone, run: failed))
        if CommandLine.arguments.contains("--verify-reply-recovered") {
            let recovered = Run(id: "recovered-reply", conversationID: "t-chief", trigger: RunTrigger.review,
                                seq: 4, createdAt: "2026-10-27T06:02:00Z", outcome: "quiet",
                                agentID: "t-chief", status: RunStatus.completed, summary: "Ready")
            events.append(Event(seq: 4, conversationID: "t-chief", type: EventType.runDone, run: recovered))
        }
        return events
    }

    private static var toolEvents: [Event] {
        let date = "2026-10-14T12:00:00Z"
        let tools = [
            ToolActivity(id: "bash", runID: "tool-run", name: "bash", status: ToolActivityStatus.completed,
                         summary: "go test ./...", output: "ok polymux 0.2s"),
            ToolActivity(id: "read-1", runID: "tool-run", name: "read", status: ToolActivityStatus.completed, summary: "one.swift"),
            ToolActivity(id: "read-2", runID: "tool-run", name: "read", status: ToolActivityStatus.completed, summary: "two.swift"),
            ToolActivity(id: "read-3", runID: "tool-run", name: "read", status: ToolActivityStatus.completed, summary: "three.swift"),
            ToolActivity(id: "edit", runID: "tool-run", name: "edit", status: ToolActivityStatus.completed, summary: "ChatPane.swift"),
            ToolActivity(id: "failed", runID: "tool-run", name: "bash", status: ToolActivityStatus.failed,
                         summary: "false", output: "Command exited with status 1\n[redacted]")
        ]
        let run = Run(id: "tool-run", conversationID: "t-chief", trigger: RunTrigger.message, tools: tools,
                      attempt: 1, seq: 1, createdAt: date, updatedAt: date, activity: RunActivity.generating,
                      agentID: "t-chief", status: RunStatus.completed, summary: "Checked the project")
        let message = Message(runID: run.id, id: "tool-message", threadID: "t-chief", role: MessageRole.assistant,
                              parts: [Part(kind: PartKind.text, text: "Checked the project and edited ChatPane.swift.")],
                              createdAt: date, status: MessageStatus.done, seq: 2, agentID: "t-chief")
        var second = message
        second.id = "tool-second"
        second.seq = 3
        second.parts = [Part(kind: PartKind.text, text: "Second reply from the same turn.")]
        return [Event(seq: 1, conversationID: "t-chief", type: EventType.runDone, createdAt: date, run: run),
                Event(seq: 2, conversationID: "t-chief", type: EventType.messageDone, message: message, createdAt: date),
                Event(seq: 3, conversationID: "t-chief", type: EventType.messageDone, message: second, createdAt: date)]
    }

    private static let profileEvents: [Event] = {
        let runs = (0...6).map { index in
            let date = WireDate.string(Date(timeIntervalSince1970: 1_790_000_000 + Double(index) * 60))
            let active = index == 0
            let run = Run(id: "profile-\(index)", conversationID: "t-chief", trigger: index == 6 ? RunTrigger.message : RunTrigger.child,
                          seq: Int64(index + 1), createdAt: date, updatedAt: date,
                          activity: active ? "Reviewing calendar" : "", agentID: "t-chief",
                          status: active ? RunStatus.running : RunStatus.completed, summary: index == 6 ? "Ordinary chat reply" : "Finished task \(index)")
            return Event(seq: run.seq, conversationID: "t-chief", type: active ? EventType.runUpdate : EventType.runDone,
                         createdAt: date, run: run)
        }
        let attachments = CommandLine.arguments.contains("--verify-profile-media") ? profileAttachments : []
        let message = Message(id: "profile-reply", threadID: "t-chief", role: MessageRole.assistant,
                              parts: attachments.map { Part(kind: $0.kind, attachment: $0) } + [Part(kind: PartKind.text, text: "Profile fixture reply")],
                              createdAt: "2026-10-14T12:00:00Z", status: MessageStatus.done, seq: 8, agentID: "t-chief")
        var events = runs + [Event(seq: 8, conversationID: "t-chief", type: EventType.messageDone, message: message,
                                   createdAt: message.createdAt)]
        if CommandLine.arguments.contains("--verify-profile-media") {
            let outgoing = Message(id: "profile-outgoing", threadID: "t-chief", role: MessageRole.user,
                                   parts: [
                                    Part(kind: AttachmentKind.image, attachment: Attachment(id: "profile-image-outgoing", kind: AttachmentKind.image,
                                         name: "Outgoing.png", mimeType: "image/png", size: Int64(profileImage.count), url: "/attachments/profile-image-outgoing/content",
                                         thumbnailURL: "/attachments/profile-thumbnail-outgoing/content")),
                                    Part(kind: AttachmentKind.video, attachment: Attachment(id: "profile-video-outgoing", kind: AttachmentKind.video,
                                         name: "Outgoing.mp4", mimeType: "video/mp4", size: Int64(profileVideo.count), url: "/attachments/profile-video-outgoing/content",
                                         thumbnailURL: "/attachments/profile-thumbnail-1/content")),
                                    Part(kind: PartKind.text, text: "Outgoing media spacing")],
                                   createdAt: "2026-10-14T12:01:00Z", status: MessageStatus.done, seq: 9)
            events.append(Event(seq: 9, conversationID: "t-chief", type: EventType.messageDone, message: outgoing, createdAt: outgoing.createdAt))
        }
        return events
    }()

    private static let profileImage = Data(base64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAKAAAADwCAIAAAAII/iNAAAB2UlEQVR42u3cwQlEIQxAwUQsxsIszE61BA96CjMl7COSwGdzzBXU1fwEAiMwAiMw//WM7VeoHDjSj+CJRmAERmBs0ZhgnEkmGIERGFs0JhiBcSaZYATGFo0JRmAExpmECbZFY4IRGIERGGcStmhPNAIjMAIjMK9bdNqiTTACIzACIzC2aEywwAiMwAiMLzrwTRaeaIERGIGxRWOCcSZhghFYYGzRmGAExpmECUZgWzQmGIERGGcSJhhbtAlGYARGYJxJ2KLxRCOwwAiMwPinO0wwAiOwwAiMLRoTjMAIjMAC44sOfJOFJxqBERiBsUWbYJxJmGAERmBs0ZhggXEmYYIRGFs0JhiBEdiZhAnGFo0JRmAERmBnErZoPNEIjMAIjMD+6Q4TjMAIjMAIjC0aEywwAiMwAuOLDnyThSdaYARGYGzRmGCcSZhggREYWzQmGIFxJmGCEdgWjQlGYATGmYQJxhZtghEYgREYZxK2aDzRAvsJBEZgBMY/3WGCERiBBUZgbNGYYARGYAQWGF904JssPNEIjMAIjC3aBONMwgQjMAJji8YEC4wzCROMwNiiMcEILDDOJEwwtmhMMAIjMAI7k7BF44lGYARGYAT2T3eYYARGYARGYK4OtI4qrlsIrbYAAAAASUVORK5CYII=")!
    private static let profileVideo = Data(base64Encoded: "AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAAPUbW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAAD6AAAAggAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAv90cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAAAAggAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAACAAAAAwAAAAAAAkZWR0cwAAABxlbHN0AAAAAAAAAAEAAAIIAAAEAAABAAAAAAJ3bWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAAAyAAAAGgBVxAAAAAAALWhkbHIAAAAAAAAAAHZpZGUAAAAAAAAAAAAAAABWaWRlb0hhbmRsZXIAAAACIm1pbmYAAAAUdm1oZAAAAAEAAAAAAAAAAAAAACRkaW5mAAAAHGRyZWYAAAAAAAAAAQAAAAx1cmwgAAAAAQAAAeJzdGJsAAAAvnN0c2QAAAAAAAAAAQAAAK5hdmMxAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAACAAMABIAAAASAAAAAAAAAABFExhdmM2My4xLjEwMiBsaWJ4MjY0AAAAAAAAAAAAAAAAGP//AAAANGF2Y0MBZAAK/+EAF2dkAAqs2UnsBEAAAAMAQAAADIPEiWWAAQAGaOvjyyLA/fj4AAAAABBwYXNwAAAAAQAAAAEAAAAUYnRydAAAAAAAADUvAAAAAAAAABhzdHRzAAAAAAAAAAEAAAANAAACAAAAABRzdHNzAAAAAAAAAAEAAAABAAAAeGN0dHMAAAAAAAAADQAAAAEAAAQAAAAAAQAACgAAAAABAAAEAAAAAAEAAAAAAAAAAQAAAgAAAAABAAAKAAAAAAEAAAQAAAAAAQAAAAAAAAABAAACAAAAAAEAAAoAAAAAAQAABAAAAAABAAAAAAAAAAEAAAIAAAAAHHN0c2MAAAAAAAAAAQAAAAEAAAANAAAAAQAAAEhzdHN6AAAAAAAAAAAAAAANAAAC0gAAAA0AAAAMAAAADAAAAAwAAAATAAAADgAAAAwAAAAMAAAAEwAAAA4AAAAMAAAADAAAABRzdGNvAAAAAAAAAAEAAAQEAAAAYXVkdGEAAABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSpdG9vAAAAHGRhdGEAAAABAAAAAExhdmY2My4xLjEwMgAAAAhmcmVlAAADfW1kYXQAAAKuBgX//6rcRem95tlIt5Ys2CDZI+7veDI2NCAtIGNvcmUgMTY1IHIzMjIyIGIzNTYwNWEgLSBILjI2NC9NUEVHLTQgQVZDIGNvZGVjIC0gQ29weWxlZnQgMjAwMy0yMDI1IC0gaHR0cDovL3d3dy52aWRlb2xhbi5vcmcveDI2NC5odG1sIC0gb3B0aW9uczogY2FiYWM9MSByZWY9MyBkZWJsb2NrPTE6MDowIGFuYWx5c2U9MHgzOjB4MTEzIG1lPWhleCBzdWJtZT03IHBzeT0xIHBzeV9yZD0xLjAwOjAuMDAgbWl4ZWRfcmVmPTEgbWVfcmFuZ2U9MTYgY2hyb21hX21lPTEgdHJlbGxpcz0xIDh4OGRjdD0xIGNxbT0wIGRlYWR6b25lPTIxLDExIGZhc3RfcHNraXA9MSBjaHJvbWFfcXBfb2Zmc2V0PS0yIHRocmVhZHM9MSBsb29rYWhlYWRfdGhyZWFkcz0xIHNsaWNlZF90aHJlYWRzPTAgbnI9MCBkZWNpbWF0ZT0xIGludGVybGFjZWQ9MCBibHVyYXlfY29tcGF0PTAgY29uc3RyYWluZWRfaW50cmE9MCBiZnJhbWVzPTMgYl9weXJhbWlkPTIgYl9hZGFwdD0xIGJfYmlhcz0wIGRpcmVjdD0xIHdlaWdodGI9MSBvcGVuX2dvcD0wIHdlaWdodHA9MiBrZXlpbnQ9MjUwIGtleWludF9taW49MjUgc2NlbmVjdXQ9NDAgaW50cmFfcmVmcmVzaD0wIHJjX2xvb2thaGVhZD00MCByYz1jcmYgbWJ0cmVlPTEgY3JmPTIzLjAgcWNvbXA9MC42MCBxcG1pbj0wIHFwbWF4PTY5IHFwc3RlcD00IGlwX3JhdGlvPTEuNDAgYXE9MToxLjAwAIAAAAAcZYiEADv//uOr+BTPcZHNePkgJI0YwqhO7v62eQAAAAlBmiRsQ3/+qWAAAAAIQZ5CeIX/NOEAAAAIAZ5hdEK/O6AAAAAIAZ5jakK/O6EAAAAPQZpoSahBaJlMCGf//qThAAAACkGehkURLC//NOEAAAAIAZ6ldEK/O6EAAAAIAZ6nakK/O6AAAAAPQZqsSahBbJlMCFf//lLAAAAACkGeykUVLC//NOEAAAAIAZ7pdEK/O6AAAAAIAZ7rakK/O6A=")!
    private static let profileFile = Data("A sample file shared in this conversation.\n".utf8)
    private static var profileAttachments: [Attachment] {
        let scrolling = CommandLine.arguments.contains("--verify-profile-scroll")
        let names = ["Plan.txt", "Notes.md"] + (scrolling ? (1...30).map { "File \($0).txt" } : [])
        return (1...(scrolling ? 30 : 6)).map {
            Attachment(id: "profile-image-\($0)", kind: AttachmentKind.image, name: "Landscape \($0).png",
                       mimeType: "image/png", size: Int64(profileImage.count), url: "/attachments/profile-image-\($0)/content",
                       thumbnailURL: "/attachments/profile-thumbnail-\($0)/content")
        } + [Attachment(id: "profile-video-demo", kind: AttachmentKind.video, name: "Demo.mp4", mimeType: "video/mp4",
                        size: Int64(profileVideo.count), url: "/attachments/profile-video-demo/content", thumbnailURL: "/attachments/profile-thumbnail-1/content")]
        + names.enumerated().map { index, name in
            Attachment(id: "profile-file-\(index)", kind: AttachmentKind.file, name: name,
                       mimeType: "text/plain", size: Int64(profileFile.count), url: "/attachments/profile-file-\(index)/content")
        }
    }

    static func failAnswers() { answers.fail() }

    static func prompt(_ name: String) -> String {
        if CommandLine.arguments.contains("--verify-profile"), name == "Chief" {
            return "Remember tomorrow’s commitments. Summarise upcoming recommendations. Review community improvements. Confirm tomorrow’s arrangements before making changes."
        }
        return "\(name) keeps the workspace on track and reports back."
    }
    static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [VerifyFixture.self]
        return URLSession(configuration: configuration)
    }
    static let activity: [Event] = {
        let thread = "t-activity"
        let at = "2026-10-13T12:00:00Z"
        let message = Message(id: "fixture-request", threadID: thread, role: MessageRole.user,
                              parts: [Part(kind: PartKind.text, text: "Review the release and remember the rollout plan.")],
                              createdAt: at, status: MessageStatus.done, seq: 1)
        let rows = [
            Activity(kind: "tool.called", verb: "Checked", objectLabel: "release status", iconOrAgentID: "terminal", at: at),
            Activity(kind: "agent.messaged", verb: "Messaged", at: at, agentIDs: ["t-chief", "t-milo"]),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "tool.called", verb: "Searched", iconOrAgentID: "magnifyingglass", at: at),
            Activity(kind: "memory.created", verb: "Saved", objectID: "rollout-plan", objectLabel: "Rollout plan", at: at)
        ]
        return [Event(seq: 1, conversationID: thread, type: EventType.messageDone, message: message, createdAt: at)]
            + rows.enumerated().map { index, row in
                Event(seq: Int64(index + 2), conversationID: thread, type: EventType.activity, agentID: "t-chief",
                      createdAt: at, payload: row)
            }
    }()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let path = request.url?.path ?? ""
        let loading = CommandLine.arguments.contains("--verify-profile-loading")
        let load = loading ? Self.profileLoads.next(path) : 0
        if CommandLine.arguments.contains("--verify-profile-media"), path.hasPrefix("/attachments/profile-") {
            let thumbnail = path.hasPrefix("/attachments/profile-thumbnail-")
            let image = thumbnail || path.hasPrefix("/attachments/profile-image-")
            let video = path.hasPrefix("/attachments/profile-video-")
            let data = video ? Self.profileVideo : image ? Self.profileImage : Self.profileFile
            let mime = video ? "video/mp4" : image ? "image/png" : "text/plain"
            if CommandLine.arguments.contains("--verify-media-slow"), !thumbnail {
                let task = Task { @Sendable [self] in
                    do {
                        try await Task.sleep(for: .seconds(8))
                        try Task.checkCancellation()
                        reply(data, mime: mime)
                    } catch {}
                }
                responseLock.withLock { responseTask = task }
            } else {
                reply(data, mime: mime)
            }
            return
        }
        if SiteFixture.handle(self) { return }
        if CommandLine.arguments.contains("--verify-providers"), path.hasPrefix("/providers") || path == "/settings" {
            let task = Task { @Sendable [self] in
                do {
                    let slow = CommandLine.arguments.contains("--verify-providers-slow") && path == "/providers" && request.httpMethod == "GET"
                    try await Task.sleep(for: slow ? .seconds(8) : .milliseconds(100))
                    let data = try Self.providers.response(request)
                    try Task.checkCancellation()
                    reply(data)
                } catch is CancellationError {
                } catch { client?.urlProtocol(self, didFailWithError: error) }
            }
            responseLock.withLock { responseTask = task }
            return
        }
        if request.httpMethod == "POST", request.url?.lastPathComponent == "messages",
           CommandLine.arguments.contains("--verify-send-slow") {
            let task = Task { @Sendable [self] in
                do {
                    try await Task.sleep(for: .seconds(5))
                    try Task.checkCancellation()
                    reply(Data())
                } catch {}
            }
            responseLock.withLock { responseTask = task }
            return
        }
        if CommandLine.arguments.contains("--verify-search-slow"), path.hasSuffix("/search") {
            let task = Task { @Sendable [self] in
                do {
                    try await Task.sleep(for: .seconds(8))
                    try Task.checkCancellation()
                    reply(Data("[]".utf8))
                } catch {}
            }
            responseLock.withLock { responseTask = task }
            return
        }
        if CardFixture.shared.handle(self) { return }
        if CredentialFixture.shared.handle(self) { return }
        if request.httpMethod == "POST", request.url?.lastPathComponent == "messages",
           CommandLine.arguments.contains("--verify-send-offline") {
            fail()
            return
        }
        // Like the server, a read receipt clears the chat's unread count for later refreshes.
        if request.httpMethod == "POST", request.url?.lastPathComponent == "read",
           let id = request.url?.deletingLastPathComponent().lastPathComponent {
            Self.sidebar.read(id)
            reply(Data("{}".utf8))
            return
        }
        if request.httpMethod == "PATCH", request.url?.path.contains("/threads/") == true {
            do {
                let data = try Self.sidebar.patch(request)
                reply(data)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
            return
        }
        if request.url?.lastPathComponent == "stream",
            ["--verify-tools", "--verify-settings", "--verify-busy", "--verify-profile", "--verify-blocked",
             "--verify-credentials"].contains(where: CommandLine.arguments.contains) {
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
                let value = CommandLine.arguments.contains("--verify-profile-update") ? Self.profiles.read(agent) : agent
                data = try JSONEncoder().encode(AgentProfile(agent: value, prompt: Self.prompt(agent.name)))
            } else {
                switch request.url?.lastPathComponent {
                case "agents":
                    let agents = CommandLine.arguments.contains("--verify-profile-update") ? Self.profiles.list(Self.agents) : Self.agents
                    data = try JSONEncoder().encode(agents)
                case "threads": data = try JSONEncoder().encode(Self.sidebar.threads)
                case "tool-runs":
                    let runs = CommandLine.arguments.contains("--verify-profile") && request.url?.path.contains("/t-chief/") == true
                        ? Self.profileEvents.compactMap(\.run).filter { $0.status == RunStatus.completed } : []
                    data = try JSONEncoder().encode(runs)
                case "configuration" where CommandLine.arguments.contains("--verify-profile"):
                    data = try JSONEncoder().encode(AgentConfiguration(
                        tools: (1...6).map { "tool_\($0)" }, skills: (1...6).map { "/skills/skill-\($0)/SKILL.md" },
                        bundledSkills: ["polymux", "report", "html-cards"]))
                case "mcp" where CommandLine.arguments.contains("--verify-profile"):
                    data = try JSONEncoder().encode((1...6).map { MCPConnection(name: "Connection \($0)", enabled: true) }
                        + [MCPConnection(name: "Disabled connection"), MCPConnection(name: "Hidden connection", enabled: true, hidden: true)])
                case "status" where CommandLine.arguments.contains("--verify-profile"):
                    data = try JSONEncoder().encode((1...4).map { index in
                        MCPStatus(name: "Connection \(index)", connected: true,
                                  tools: (1...3).map { "connection_\(index)_tool_\($0)" })
                    }
                        + (5...6).map { MCPStatus(name: "Connection \($0)", connected: false) })
                case "proactivity" where CommandLine.arguments.contains("--verify-profile"):
                    data = try JSONEncoder().encode(Proactivity(enabled: true, intervalSeconds: 14400))
                case "routines" where CommandLine.arguments.contains("--verify-profile"):
                    let routines = (1...4).map {
                        Routine(nextAt: "2026-10-24T09:00:00Z", status: RoutineStatus.active, id: "routine-\($0)",
                                agentID: "t-chief", trigger: RoutineTrigger.once,
                                prompt: "\(loading && load > 1 ? "Updated scheduled" : "Scheduled") check \($0)")
                    }
                    data = try JSONEncoder().encode(CommandLine.arguments.contains("--verify-profile-empty") ? [] : routines)
                case "sidebar":
                    let placements = Self.sidebar.threads.map {
                        SidebarPlacement(threadID: $0.id, pinned: $0.pinned ?? false, pinOrder: $0.pinOrder,
                                         sectionID: $0.sectionID, sectionOrder: $0.sectionOrder)
                    }
                    data = try JSONEncoder().encode(SidebarSnapshot(cursor: 0, sections: [Section(id: SidebarLayout.unassignedID, name: "Unassigned", order: 0)], placements: placements))
                case "settings":
                    let theme = CommandLine.arguments.contains("--verify-tools")
                        ? (CommandLine.arguments.contains("--tools-dark") ? "dark" : "light") : Self.appearance
                    data = try JSONEncoder().encode(Settings(appearance: theme, sidebarWidth: 360))
                case "network":
                    if CommandLine.arguments.contains("--verify-network-fail") { fail(); return }
                    if CommandLine.arguments.contains("--verify-network-slow") { return }
                    let devices: [[String: Any]] = [
                        ["id": "d-mac", "name": "Home server", "paired": true],
                        ["id": "d-ios", "name": "Carlvince’s iPhone", "paired": true],
                        ["id": "d-old", "name": "Spare MacBook Pro used for development and device-pairing verification", "label": "Spare device", "paired": true],
                    ]
                    data = try JSONSerialization.data(withJSONObject: [
                        "server": ["name": "Home server", "build": "0.4.0", "platform": "macOS", "role": "server"],
                        "devices": devices,
                        "current": "d-ios",
                        "host": "d-mac",
                        "online": ["d-mac"],
                    ])
                case "invite":
                    data = try JSONEncoder().encode(Invite(url: "http://192.168.1.20:7337", token: "fixture-token",
                                                           ticket: "endpoint" + String(repeating: "a", count: 140)))
                case "messages":
                    if CommandLine.arguments.contains("--verify-reply-error"), path.contains("/t-chief/") {
                        data = try JSONEncoder().encode(Self.replyEvents.compactMap(\.message))
                        break
                    }
                    // Pages like the server: the latest 100 before the cursor, oldest first.
                    let before = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?
                        .first { $0.name == "before" }?.value.flatMap(Int64.init) ?? .max
                    let history = CommandLine.arguments.contains("--verify-history") && request.url?.path.contains("/t-long/") == true
                    let page = history ? Self.history.compactMap(\.message).filter { $0.seq < before }.suffix(100) : []
                    data = try JSONEncoder().encode(Array(page))
                case "events":
                    let after = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?
                        .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
                    let seeded: [Event]
                    if CommandLine.arguments.contains("--verify-reply-error"), path.contains("/t-chief/") {
                        seeded = Self.replyEvents
                    } else if CommandLine.arguments.contains("--verify-blocked"),
                       let thread = Self.threads.first(where: { request.url?.path.contains("/\($0.id)/") == true }) {
                        seeded = thread.runs.map {
                            Event(seq: $0.seq, conversationID: thread.id, type: EventType.runUpdate, createdAt: $0.updatedAt, run: $0)
                        }
                    } else if CommandLine.arguments.contains("--verify-profile"), request.url?.path.contains("/t-chief/") == true {
                        seeded = Self.profileEvents
                    } else if CommandLine.arguments.contains("--verify-tools"), request.url?.path.contains("/t-chief/") == true {
                        seeded = Self.toolEvents
                    } else if CommandLine.arguments.contains("--verify-stream-preview"), request.url?.path.contains("/t-chief/") == true {
                        seeded = Self.streamPreviewEvents
                    } else if CommandLine.arguments.contains("--verify-busy"),
                               let busy = (CommandLine.arguments.contains("--verify-actions")
                                    ? ["grp-trip", "t-chief", "t-aria", "t-milo", "t-nova"] : ["grp-trip", "t-chief"])
                                    .first(where: { request.url?.path.contains("/\($0)/") == true }) {
                        seeded = Self.busyEvents(busy)
                    } else if CommandLine.arguments.contains("--verify-history"), request.url?.path.contains("/t-long/") == true {
                        seeded = Self.history
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
            let detail = path == "/agents/t-chief" || ["configuration", "mcp", "providers"].contains(request.url?.lastPathComponent ?? "")
            if loading, detail || (request.url?.lastPathComponent == "routines" && load > 2) {
                let task = Task { @Sendable [self, data] in
                    do {
                        try await Task.sleep(for: .seconds(8))
                        try Task.checkCancellation()
                        reply(data)
                    } catch {}
                }
                responseLock.withLock { responseTask = task }
            } else {
                reply(data)
            }
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    private func reply(_ data: Data, mime: String = "application/json") {
        let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil,
                                       headerFields: ["Content-Type": mime])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {
        responseLock.withLock { responseTask?.cancel() }
        Self.answers.remove(self)
        CardFixture.shared.remove(self)
    }

    private func fail() {
        client?.urlProtocol(self, didFailWithError: URLError(.cannotConnectToHost))
    }

    private final class ProfileLoads: @unchecked Sendable {
        private let lock = NSLock()
        private var counts: [String: Int] = [:]

        func next(_ path: String) -> Int {
            lock.withLock {
                counts[path, default: 0] += 1
                return counts[path, default: 0]
            }
        }
    }

    private final class Profiles: @unchecked Sendable {
        private let lock = NSLock()
        private var opened = false

        func read(_ agent: Agent) -> Agent {
            lock.withLock {
                let result = value(agent)
                if agent.id == "t-chief" { opened = true }
                return result
            }
        }

        func list(_ agents: [Agent]) -> [Agent] { lock.withLock { agents.map(value) } }

        private func value(_ agent: Agent) -> Agent {
            guard opened, agent.id == "t-chief" else { return agent }
            var agent = agent
            agent.name = "Alan"
            agent.role = "Researcher"
            return agent
        }
    }

    private final class Sidebar: @unchecked Sendable {
        private struct Placement: Decodable { let sectionID: String? }
        private let lock = NSLock()
        private var values = VerifyFixture.threads.map { thread in
            var thread = thread
            let busy = CommandLine.arguments.contains("--verify-actions")
                ? ["t-chief", "t-aria", "t-milo", "t-nova", "grp-trip"] : ["t-chief", "grp-trip"]
            if CommandLine.arguments.contains("--verify-profile"), thread.id == "t-chief" {
                thread.runs = VerifyFixture.profileEvents.compactMap(\.run)
            } else if CommandLine.arguments.contains("--verify-stream-preview"), thread.id == "t-chief" {
                thread.runs = VerifyFixture.streamPreviewEvents.compactMap(\.run)
            } else if CommandLine.arguments.contains("--verify-busy"), busy.contains(thread.id) {
                thread.runs = VerifyFixture.busyEvents(thread.id).compactMap(\.run)
            }
            if CommandLine.arguments.contains("--verify-pins"), ["t-chief", "t-aria"].contains(thread.id) {
                thread.pinned = true
                thread.pinOrder = thread.id == "t-chief" ? 0 : 1
                thread.unreadCount = thread.id == "t-aria" ? 3 : 0
            }
            return thread
        }

        var threads: [PolymuxKit.Thread] { lock.withLock { values } }

        func read(_ id: String) {
            lock.withLock {
                if let index = values.firstIndex(where: { $0.id == id }) { values[index].unreadCount = 0 }
            }
        }

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
            let placement = request.url?.lastPathComponent == "placement"
            let sectionID = placement ? try JSONDecoder().decode(Placement.self, from: body).sectionID : nil
            let id = placement ? request.url?.deletingLastPathComponent().lastPathComponent : request.url?.lastPathComponent
            return try lock.withLock {
                guard let index = values.firstIndex(where: { $0.id == id }) else {
                    throw URLError(.resourceUnavailable)
                }
                if let pinned = patch.pinned {
                    values[index].pinned = pinned
                    values[index].pinOrder = pinned ? patch.pinOrder : nil
                }
                if placement {
                    values[index].sectionID = sectionID
                    values[index].sectionOrder = (values.filter { $0.sectionID == sectionID && $0.pinned != true }
                        .compactMap(\.sectionOrder).max() ?? -1) + 1
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
    private let part = Part(kind: PartKind.choice, question: "What should we focus on today?", options: [
        ChoiceOption(id: "day", label: "Plan my day"),
        ChoiceOption(id: "code", label: "Build something")
    ])
    private let client = APIClient(baseURL: URL(string: "http://approval.invalid")!,
                                   session: VerifyFixture.session(), token: "")

    var body: some View {
        VStack(spacing: 16) {
            ChoiceCard(part: part, client: client,
                       message: Message(id: "choice:t-approval", threadID: "t-approval", role: MessageRole.assistant, parts: [part], createdAt: "", status: MessageStatus.done, agentID: "t-approval"),
                       bubble: BubbleStyle.incoming(scheme))
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
