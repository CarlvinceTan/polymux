#if DEBUG
import Foundation
import PolymuxKit

/// The plugin routes for UI checks: --verify-plugins, empty with --verify-plugins-empty.
final class PluginFixture: @unchecked Sendable {
    private let lock = NSLock()
    private var layout = PluginLayout(switcher: PluginFixture.graphOn ? ["chats"] : ["chats", "hub"],
                                      buttons: PluginFixture.webOn ? [["canvas"]] : PluginFixture.graphOn ? [["graph-visualiser"]] : PluginFixture.permissions ? [["permissions"]] : [["notes", "tasks"], ["drive"]])
    private var installing: String?
    private var rules = PermissionRules()
    private static let permissions = CommandLine.arguments.contains("--verify-permissions")
    static let graphOn = CommandLine.arguments.contains("--verify-graph")
    /// A full working day rather than the minimum the checks need: every kind of node, with things moving.
    static let graphBusy = CommandLine.arguments.contains("--verify-graph-busy") || GraphSceneFixture.crowd
    /// Any of the scenes that dress the bots up.
    static var graphScene: Bool { graphBusy || GraphSceneFixture.video }
    static let cast: [String: (role: String, shape: String, color: String)] = [
        "Chief": ("Chief of staff", "fox", "orange"), "Aria": ("Researcher", "whale", "bleu"), "Milo": ("Designer", "strawberry", "rose"),
        "Nova": ("Engineer", "robot", "violet"), "Approval": ("Reviewer", "penguin", "turquoise"), "Fern": ("Gardener", "fern", "vert"),
        "Juno": ("Support", "cat", "ambre"), "Kai": ("Planner", "dog", "brun"), "Lumi": ("Writer", "rabbit", "rose"),
        "Otto": ("Builder", "bear", "rouge"), "Pia": ("Assistant", "duck", "ambre"), "Rex": ("Analyst", "frog", "vert"),
    ]
    static let webOn = CommandLine.arguments.contains("--verify-plugin-web")
    private static let page = """
    <!doctype html><html><head><title>Canvas</title><meta name="viewport" content="width=device-width,initial-scale=1"><style>
    html, body { margin: 0; height: 100%; background: #2f6f5e; color: #fff; font: 15px system-ui; }
    body { padding: 64px 16px 16px; box-sizing: border-box; }
    </style></head><body><h1>Fixture page</h1>
    <button onclick="fetch('api/ping').then(r => r.json()).then(j => document.querySelector('p').textContent = j.answer + ' in ' + polymux.chat)">Ping</button>
    <button onclick="polymux.close()">Finish</button><p></p></body></html>
    """
    private var installed: [Plugin] = PluginFixture.webOn ? [
        Plugin(name: "canvas", title: "Canvas", description: "A page of its own.", symbol: "paintbrush", selectedSymbol: "paintbrush.fill",
               version: "0.1.0", kind: PluginKind.scoped, surface: "web", page: "index.html",
               hideClose: CommandLine.arguments.contains("--verify-plugin-noclose"), state: PluginState.ready, source: "Made by Aria", chats: ["t-chief"]),
    ] : PluginFixture.graphOn ? [
        Plugin(name: "graph-visualiser", title: "Graph Visualiser", description: "See how your bots, people, devices and repositories work together.",
               symbol: "point.3.connected.trianglepath.dotted", selectedSymbol: "point.3.filled.connected.trianglepath.dotted", version: "0.1.0",
               kind: PluginKind.both, surface: "graph-visualiser", state: PluginState.ready, source: "example/graph", chats: ["t-chief"]),
    ] : PluginFixture.permissions ? [
        Plugin(name: "permissions", title: "Permissions", description: "Set rules a chat's bots can't break.",
               symbol: "lock.shield", selectedSymbol: "lock.shield.fill", version: "0.1.0", kind: PluginKind.scoped,
               surface: "permissions", state: PluginState.ready, source: "example/permissions", chats: ["t-chief"]),
    ] : CommandLine.arguments.contains("--verify-plugins-empty") ? [] : [
        PluginFixture.hub(state: PluginState.ready),
        PluginFixture.scoped("notes", "Notes", "doc.text", chats: ["t-chief", "grp-trip"]),
        PluginFixture.scoped("tasks", "Tasks", "checkmark.circle", chats: ["t-chief"]),
        PluginFixture.scoped("drive", "Drive", "externaldrive", chats: ["t-chief", "t-aria"]),
        Plugin(name: "quiet", title: "Quiet", description: "Adds a skill.", symbol: "sparkles", version: "1.0.0",
               kind: PluginKind.scoped, state: PluginState.ready, source: "example/quiet"),
    ]

    private static func hub(state: String) -> Plugin {
        Plugin(name: "hub", title: "Hub", description: "Your messages and mail in one place.",
               symbol: "tray.2", selectedSymbol: "tray.2.fill", version: "1.0.0", kind: PluginKind.global, surface: "hub",
               state: state, source: "CarlvinceTan/polymux@1234567")
    }

    private static func scoped(_ name: String, _ title: String, _ symbol: String, chats: [String]) -> Plugin {
        Plugin(name: name, title: title, description: "\(title) for a chat.", symbol: symbol, selectedSymbol: symbol + ".fill", version: "1.0.0",
               kind: PluginKind.scoped, surface: SampleSurface.name, state: PluginState.ready,
               source: "example/\(name)", chats: chats)
    }

    private var market: [MarketPlugin] {
        [MarketPlugin(name: "hub", title: "Hub", description: "Your messages and mail in one place.", symbol: "tray.2",
                      author: "Polymux", kind: PluginKind.global, surface: "hub", version: "1.0.0",
                      source: "CarlvinceTan/polymux@1234567", compatible: true,
                      installed: installed.contains { $0.name == "hub" } ? "1.0.0" : nil)]
    }

    private static let at = Int64(Date.now.timeIntervalSince1970 * 1000)
    private static let chats = [
        HubChat(id: "!priya", platform: "whatsapp", name: "Priya Raman", avatar: "", group: false, space: false,
                preview: "The quarterly numbers are attached.", at: at - 60_000, unread: 3),
        HubChat(id: "!team", platform: "slack", name: "Launch team", avatar: "", group: true, space: false,
                preview: "Shipping Thursday", at: at - 3_600_000, unread: 124),
        HubChat(id: "!sam", platform: "telegram", name: "Sam", avatar: "", group: false, space: false,
                preview: "See you soon", at: at - 90_000_000, unread: 0),
        HubChat(id: "!wechat", platform: "wechat", name: "Family", avatar: "", group: true, space: false,
                preview: "Dinner?", at: at - 7_000_000, unread: 0),
        HubChat(id: "!acme", platform: "slack", name: "Acme", avatar: "", group: true, space: true, preview: "", at: 0, unread: 0),
        HubChat(id: "!general", platform: "slack", name: "general", avatar: "", group: true, space: false, parents: ["!acme"],
                preview: "Standup moved", at: at - 7_200_000, unread: 2),
        HubChat(id: "!random", platform: "slack", name: "random", avatar: "", group: true, space: false, parents: ["!acme"],
                preview: "Lunch?", at: at - 8_200_000, unread: 5),
    ]

    private static let thread = [
        HubMessage(id: "$1", chat: "!priya", sender: "@priya", senderName: "Priya Raman", mine: false,
                   text: "Morning! Did you see the deck?", at: at - 120_000, kind: HubMessageKind.text),
        HubMessage(id: "$2", chat: "!priya", sender: "@me", senderName: "Me", mine: true,
                   text: "Yes, looks good.", at: at - 90_000, kind: HubMessageKind.text,
                   readers: [HubMember(id: "@priya", name: "Priya Raman", avatar: "")]),
        HubMessage(id: "$3", chat: "!priya", sender: "@priya", senderName: "Priya Raman", mine: false,
                   text: "The quarterly numbers are attached.", at: at - 60_000, kind: HubMessageKind.text),
    ]
    /// The page behind the first, so a search has to reach further than what's on screen.
    private static let older = [
        HubMessage(id: "$0", chat: "!priya", sender: "@priya", senderName: "Priya Raman", mine: false,
                   text: "Budget notes are at https://www.example.com/budget", at: at - 86_400_000, kind: HubMessageKind.text),
    ]
    /// A chat list taller than the sidebar: --verify-hub-long.
    private static var fillerChats: [HubChat] {
        guard CommandLine.arguments.contains("--verify-hub-long") else { return [] }
        return (1...30).map { index in
            HubChat(id: "!filler-\(index)", platform: "telegram", name: "Chat \(index)", avatar: "", group: false, space: false,
                    preview: "Filler", at: at - Int64(index) * 200_000_000, unread: 0)
        }
    }

    /// A history taller than the screen, so messages reach the header: --verify-hub-long.
    private static func filler(_ chat: String) -> [HubMessage] {
        guard CommandLine.arguments.contains("--verify-hub-long") else { return [] }
        return (1...40).map { index in
            let mine = index % 3 == 0
            var message = HubMessage(id: "$filler-\(index)", chat: chat, sender: mine ? "@me" : "@priya", senderName: mine ? "Me" : "Priya Raman", mine: mine,
                                     text: "Message \(index), long enough to wrap onto a second line of its bubble on a phone.",
                                     at: at - 7_200_000 + Int64(index), kind: HubMessageKind.text)
            if index == 40 {
                message.replyTo = "$filler-2"
                message.replyText = "Message 2, the one this answers."
                message.replySender = "Priya Raman"
            }
            return message
        }
    }
    private static let members = [HubMember(id: "@slack_1:hub", name: "Priya Raman", avatar: ""),
                                  HubMember(id: "@slack_2:hub", name: "Sam Lee", avatar: "")]
    private var reactions = [HubReaction(key: "👍", count: 2, mine: true, people: [HubMember(id: "@me", name: "You", avatar: ""),
                                                                                  HubMember(id: "@priya", name: "Priya Raman", avatar: "")]),
                             HubReaction(key: "🎉", count: 1, mine: false, people: [HubMember(id: "@sam", name: "Sam Lee", avatar: "")])]
    private var sent: [HubMessage] = []
    private var selector = HubSettingsSelector.text
    private var linked: [HubAccount] = []
    private static let people = [HubPerson(id: "tom", platform: "whatsapp", account: "a", name: "Tom Chen"),
                                 HubPerson(id: "ana", platform: "whatsapp", account: "a", name: "Ana Ruiz"),
                                 HubPerson(id: "sam", platform: "telegram", account: "a", name: "Sam", chat: "!sam")]
    private var teamSent: [HubMessage] = []
    private var names: [String: String] = [:]
    private var incognito = false
    private var readChats: Set<String> = []
    private var revision: Int64 = 1
    private var mail: [HubMailAccount] = []
    private let mailFixture = HubMailFixture()
    private var clients: Set<String> = ["google"]

    /// Chief hands Aria a task and runs Claude Code; Aria drives Safari, and Milo is idle.
    private static var work: WorkActivity {
        let stamp = "2026-11-01T00:00:00Z"
        func run(_ id: String, agent: String, chat: String, trigger: String = RunTrigger.message, status: String = RunStatus.running,
                 parent: String? = nil, summary: String = "", external: ExternalAgent? = nil, tools: [ToolActivity]) -> Run {
            Run(jobID: id, id: id, conversationID: chat, trigger: trigger, parentRunID: parent, tools: tools, attempt: 1, seq: 1,
                createdAt: stamp, updatedAt: stamp, activity: "", agentID: agent, status: status, summary: summary, external: external)
        }
        func tool(_ id: String, _ name: String, _ summary: String, status: String = ToolActivityStatus.completed) -> ToolActivity {
            ToolActivity(id: id, runID: "run", name: name, status: status, summary: summary)
        }
        func delivery(_ id: String, _ from: String, _ to: String, _ intent: String, _ status: String, run: String) -> PeerDelivery {
            PeerDelivery(id: id, sourceAgentID: from, targetAgentID: to, conversationID: from, parentRunID: run, intent: intent, status: status, createdAt: stamp)
        }
        let running = ToolActivityStatus.running
        if GraphSceneFixture.video { return GraphSceneFixture.videoWork }
        if graphBusy {
            return WorkActivity(build: "abc1234", runs: (GraphSceneFixture.crowd ? GraphSceneFixture.crowdRuns : []) + [
                run("run-chief", agent: "t-chief", chat: "t-chief", summary: "Shipping the sign-in fix", tools: [
                    tool("claude", "bash", "claude -p 'Fix the sign-in bug'", status: running),
                    tool("c-edit", "edit", "apps/server/auth.go", status: running),
                    tool("c-read", "read", "docs/sign-in.md"),
                    tool("c-test", "bash", "go test ./..."),
                ]),
                run("run-aria", agent: "t-aria", chat: "t-aria", trigger: RunTrigger.peer, summary: "Researching sign-in flows", tools: [
                    tool("a-web", "http_request", "api.github.com/repos/example/polymux/issues", status: running),
                    tool("a-csv", "read", "research/competitors.csv"),
                    tool("a-notes", "write", "research/findings.md"),
                    tool("a-pdf", "read", "research/passkeys.pdf"),
                ]),
                run("run-milo", agent: "t-milo", chat: "t-milo", trigger: RunTrigger.peer, summary: "Drawing the new sign-in screen", tools: [
                    tool("m-image", "generate_image", "Sign-in hero", status: running),
                    tool("m-video", "generate_video", "Walkthrough"),
                    tool("m-png", "write", "design/hero.png"),
                    tool("m-mov", "write", "design/walkthrough.mp4"),
                    tool("m-voice", "write", "design/voiceover.m4a"),
                ]),
                run("run-codex", agent: "t-nova", chat: "t-nova", trigger: RunTrigger.child, parent: "run-nova", summary: "Profiling the build",
                    external: ExternalAgent(agent: "codex-acp", name: "Codex"), tools: []),
                run("run-nova", agent: "t-nova", chat: "t-nova", status: RunStatus.waitingInput, summary: "Waiting on a question", tools: [
                    tool("n-build", "bash", "swift build", status: ToolActivityStatus.failed),
                    tool("n-swift", "edit", "apps/Polymux/Sources/SignIn.swift"),
                    tool("n-json", "write", "bench/metrics.json"),
                ]),
            ], deliveries: [
                delivery("task", "t-chief", "t-aria", PeerDeliveryIntent.request, PeerDeliveryStatus.delivered, run: "run-chief"),
                delivery("result", "t-aria", "t-chief", PeerDeliveryIntent.result, PeerDeliveryStatus.completed, run: "run-aria"),
                delivery("draw", "t-chief", "t-milo", PeerDeliveryIntent.request, PeerDeliveryStatus.delivered, run: "run-chief"),
                delivery("ask", "t-nova", "t-chief", PeerDeliveryIntent.question, PeerDeliveryStatus.queued, run: "run-nova"),
            ])
        }
        return WorkActivity(build: "abc1234", runs: [
            run("run-chief", agent: "t-chief", chat: "t-chief", tools: [
                tool("claude", "bash", "claude -p 'Fix the sign-in bug'", status: ToolActivityStatus.running),
                tool("read", "read", "apps/server/main.go"),
            ]),
            run("run-aria", agent: "t-aria", chat: "t-aria", trigger: RunTrigger.peer, tools: [tool("open", "bash", "git status")]),
        ], deliveries: [
            PeerDelivery(id: "task", sourceAgentID: "t-chief", targetAgentID: "t-aria", conversationID: "t-chief", parentRunID: "run-chief",
                         intent: PeerDeliveryIntent.request, status: PeerDeliveryStatus.delivered, createdAt: stamp),
            PeerDelivery(id: "result", sourceAgentID: "t-aria", targetAgentID: "t-chief", conversationID: "t-aria", parentRunID: "run-aria",
                         intent: PeerDeliveryIntent.result, status: PeerDeliveryStatus.completed, createdAt: stamp),
        ])
    }

    private static var graph: WorkGraph {
        if GraphSceneFixture.video { return GraphSceneFixture.videoGraph }
        var nodes = [
            WorkNode(id: "safari", title: "Safari", parent: "agent:t-aria", kind: "app", symbol: "safari", state: WorkNodeState.working,
                     blocks: [WorkBlock(kind: WorkBlockKind.progress, text: "Loading", value: 0.6), WorkBlock(kind: WorkBlockKind.badge, text: "2 tabs")],
                     chats: ["t-aria"]),
            WorkNode(id: "tab-docs", title: "Polymux docs", parent: "safari", kind: "tab", symbol: "doc.text", chats: ["t-aria"]),
            WorkNode(id: "tab-issues", title: "Open issues", parent: "safari", kind: "tab", symbol: "exclamationmark.circle", chats: ["t-aria"]),
            WorkNode(id: "repo:polymux", title: "polymux", parent: WorkGraphBuilder.serverDevice, kind: "repo", subtitle: "dev at 7e36d11",
                     blocks: [WorkBlock(kind: WorkBlockKind.badge, text: "3 changes", tint: WorkBlockTint.orange)],
                     details: [WorkDetail(label: "Path", value: "~/code/proj/polymux"), WorkDetail(label: "Branch", value: "dev")]),
            WorkNode(id: "github:example/polymux", title: "example/polymux", kind: "github", symbol: "arrow.triangle.branch"),
            WorkNode(id: "github:example/polymux#7", title: "Add the graph", parent: "github:example/polymux", kind: "pull", subtitle: "Pull request 7", symbol: "arrow.triangle.pull", state: WorkNodeState.working),
        ]
        var wires = [
            WorkWire(id: "repo-github", from: "repo:polymux", to: "github:example/polymux", direction: WorkWireDirection.both, line: WorkWireLine.dashed),
            WorkWire(id: "chief-repo", from: "agent:t-chief", to: "repo:polymux", label: "Works in", chats: ["t-chief"]),
        ]
        if graphBusy {
            nodes += [
                WorkNode(id: "figma", title: "Figma", parent: "agent:t-milo", kind: WorkKind.app, symbol: "paintbrush.pointed", tint: WorkNodeTint.purple,
                         state: WorkNodeState.working, chats: ["t-milo"]),
                WorkNode(id: "figma-file", title: "Sign-in screens.fig", parent: "figma", kind: WorkKind.design, subtitle: "3 frames", chats: ["t-milo"]),
                WorkNode(id: "db:users", title: "users.db", parent: WorkGraphBuilder.serverDevice, kind: WorkKind.data, subtitle: "SQLite"),
            ]
            wires += [
                WorkWire(id: "claude-repo", from: "worker:claude", to: "repo:polymux", state: WorkWireState.active, label: "Fixing the sign-in bug", carries: WorkKind.code,
                         chats: ["t-chief"]),
                WorkWire(id: "milo-figma", from: "agent:t-milo", to: "figma", direction: WorkWireDirection.none, state: WorkWireState.active, label: "Laying out 3 frames", carries: WorkKind.design,
                         chats: ["t-milo"]),
                WorkWire(id: "nova-db", from: "agent:t-nova", to: "db:users", label: "Queries", carries: WorkKind.table, chats: ["t-nova"]),
            ]
        }
        if GraphSceneFixture.crowd {
            nodes += GraphSceneFixture.crowdNodes
            wires += GraphSceneFixture.crowdWires
        }
        if CommandLine.arguments.contains("--verify-graph-long") {
            nodes.append(WorkNode(id: "long", title: "A title long enough to need truncating on every card it appears on, in any language", subtitle: "With a subtitle just as long, which has to give way before the title does"))
        }
        return WorkGraph(revision: 1, nodes: nodes, wires: wires)
    }

    /// WhatsApp chats split between two linked accounts, with one group both are in.
    private static var accountChats: [HubChat] {
        [("!home", "Home group", ["a"]), ("!office", "Office group", ["b"]), ("!shared", "Family and friends", ["a", "b"])].map { id, name, accounts in
            HubChat(id: id, platform: "whatsapp", name: name, avatar: "", group: true, space: false, parents: [], preview: "", at: 1, unread: id == "!office" ? 2 : 0, accounts: accounts)
        }
    }

    private func hub(_ request: URLRequest, path: String) throws -> Data {
        let encoder = JSONEncoder()
        let method = request.httpMethod ?? "GET"
        if method == "GET", path.hasSuffix("/messages"), CommandLine.arguments.contains("--verify-hub-slow") {
            Thread.sleep(forTimeInterval: 4)
        }
        if HubMailFixture.enabled, path.hasPrefix("mail/"), let answer = try mailFixture.answer(request, method: method, path: path, body: { try body(request) }) {
            return answer
        }
        switch (method, path) {
        case ("GET", "status"):
            let account = [HubAccount(id: "a", name: "Personal")]
            if HubMailFixture.enabled {
                linked = [HubAccount(id: "b", name: "Work")]
                mail = mailFixture.accounts
            }
            let platforms = [HubPlatform(id: "whatsapp", name: "WhatsApp", state: HubPlatformState.running, accounts: account + linked,
                                         flows: [HubFlow(id: "qr", name: "Scan a code"), HubFlow(id: "phone", name: "Phone number")]),
                             HubPlatform(id: "slack", name: "Slack", state: HubPlatformState.running, accounts: account),
                             HubPlatform(id: "telegram", name: "Telegram", state: HubPlatformState.running, accounts: account),
                             HubPlatform(id: "signal", name: "Signal", state: HubPlatformState.stopped)]
            let fields = [HubField(id: "clientId", name: "Client ID"), HubField(id: "clientSecret", name: "Client secret", secret: true)]
            let providers = [("google", "Google"), ("microsoft", "Microsoft")].map { id, name in
                HubMailProvider(id: id, name: name, setup: clients.contains(id) ? [] : fields)
            }
            return try encoder.encode(HubStatus(state: HubStatusState.ready, revision: 1, platforms: platforms, mail: mail,
                                                incognito: incognito, mailProviders: providers, selector: selector, textOnly: [], wechatRename: true,
                                                mailPresets: [HubMailPreset(id: "gmail", name: "Gmail / Google Workspace", imapHost: "imap.gmail.com", imapPort: 993, smtpHost: "smtp.gmail.com", smtpPort: 587,
                                                                            hint: "Needs a 16-character app password from your Google account.", hidden: false),
                                                              HubMailPreset(id: "custom", name: "Other (IMAP/SMTP)", imapHost: "", imapPort: 993, smtpHost: "", smtpPort: 587, hint: "Enter the server names.", hidden: false)]))
        case ("GET", "people"):
            return try encoder.encode(Self.people)
        case ("POST", "chats"):
            let input = try JSONDecoder().decode(HubChatCreate.self, from: body(request))
            return try encoder.encode(["chat": input.people.count > 1 ? "!team" : "!sam"])
        case ("POST", "platforms/whatsapp/logins"):
            return try encoder.encode(HubLoginStep(login: "login", step: "phone", kind: HubLoginStepKind.input, instructions: "Enter your number.",
                                                   fields: [HubField(id: "phone", name: "Phone")]))
        case ("POST", "platforms/whatsapp/logins/login/steps/phone"):
            linked = [HubAccount(id: "b", name: "Work")]
            return try encoder.encode(HubLoginStep(login: "login", step: "phone", kind: HubLoginStepKind.done))
        case ("DELETE", "platforms/whatsapp/accounts/b"):
            linked = []
            return Data("{}".utf8)
        case ("GET", "chats"):
            let chats = Self.chats.map { chat in
                var value = chat
                value.name = names[chat.id] ?? chat.name
                return value
            }
            let all = chats + Self.fillerChats + (HubMailFixture.enabled ? Self.accountChats : [])
            return try encoder.encode(HubChanges(revision: revision, chats: all.map { chat in
                var value = chat
                if readChats.contains(chat.id) { value.unread = 0 }
                return value
            }))
        case ("POST", _) where path.hasSuffix("/read"):
            if let chat = path.split(separator: "/").dropFirst().first {
                readChats.insert(String(chat))
                revision += 1
            }
            return Data()
        case ("GET", "chats/!priya/messages"):
            if request.url?.query?.contains("before") == true { return try encoder.encode(HubPage(messages: Self.older)) }
            let thread = Self.thread.map { message in
                var value = message
                if value.id == "$2" { value.reactions = reactions }
                return value
            }
            return try encoder.encode(HubPage(messages: Self.filler("!priya") + thread + sent, before: "older"))
        case ("POST", "chats/!priya/messages/$2/reactions"):
            reactions = HubReactions.toggled(reactions, key: try JSONDecoder().decode(HubReact.self, from: body(request)).key)
            return Data()
        case ("POST", "chats/!priya/messages"):
            let input = try JSONDecoder().decode(HubSend.self, from: body(request))
            let kind = input.sticker == true ? HubMessageKind.sticker : input.attachment?.voice == true ? HubMessageKind.audio : HubMessageKind.text
            sent.append(HubMessage(id: "$sent-\(sent.count)", chat: "!priya", sender: "@me", senderName: "Me", mine: true,
                                   text: input.text, at: Self.at, kind: kind, attachment: input.attachment))
            return Data()
        case ("GET", "chats/!team/messages"):
            return try encoder.encode(HubPage(messages: Self.filler("!team") + teamSent))
        case ("POST", "chats/!team/messages"):
            let input = try JSONDecoder().decode(HubSend.self, from: body(request))
            teamSent.append(HubMessage(id: "$team-\(teamSent.count)", chat: "!team", sender: "@me", senderName: "Me", mine: true,
                                       text: input.text, at: Self.at, kind: HubMessageKind.text))
            let targets = input.mentions?.users.map(\.id).joined(separator: ", ") ?? ""
            teamSent.append(HubMessage(id: "$note-\(teamSent.count)", chat: "!team", sender: "@bot", senderName: "Bridge", mine: false,
                                       text: "Mentioned \(targets)", at: Self.at, kind: HubMessageKind.notice))
            return Data()
        case ("GET", "chats/!team/members"):
            return try encoder.encode(Self.members)
        case ("PUT", "chats/!team/name"):
            names["!team"] = try JSONDecoder().decode(HubRename.self, from: body(request)).name
            return Data()
        case ("PUT", "settings"):
            incognito = try JSONDecoder().decode(HubSettings.self, from: body(request)).incognito
            selector = try JSONDecoder().decode(HubSettings.self, from: body(request)).selector ?? selector
            return Data()
        case ("POST", "mail/oauth/google"):
            return try encoder.encode(HubMailSignIn(id: "fixture", url: "https://accounts.example/authorize"))
        case ("POST", "mail/oauth/google/sessions/fixture"):
            let account = HubMailAccount(id: "mail-1", name: "me@gmail.example", email: "me@gmail.example")
            mail = [account]
            return try encoder.encode(account)
        case ("PUT", "mail/oauth/microsoft/client"):
            clients.insert("microsoft")
            return Data()
        case ("GET", "links"):
            return try encoder.encode([HubLink]())
        case ("GET", "broadcasts"):
            return try encoder.encode([HubBroadcast]())
        case ("GET", "stickers"):
            return try encoder.encode([HubAttachment(uri: "mxc://fixture/wave", name: "Wave", mime: "image/png", size: 1, width: 64, height: 64)])
        case ("POST", "media"):
            return try encoder.encode(["uri": "mxc://fixture/voice"])
        case ("GET", _) where path.hasSuffix("/members"):
            return try encoder.encode([HubMember]())
        case ("GET", _) where path.hasSuffix("/messages"):
            return try encoder.encode(HubPage())
        default:
            return try encoder.encode([String: String]())
        }
    }

    func response(_ request: URLRequest) throws -> Data {
        try lock.withLock {
            let path = request.url!.path
            let name = request.url!.lastPathComponent
            let encoder = JSONEncoder()
            if path == "/work" { return try encoder.encode(Self.work) }
            if path == "/plugins/canvas/ui/index.html" { return Data(Self.page.utf8) }
            if path == "/plugins/canvas/api/ping" { return Data(#"{"answer":"Service answered"}"#.utf8) }
            if path == "/plugins/graph-visualiser/api/preview" {
                return GraphSceneFixture.picture(URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first?.value ?? "")
            }
            if path == "/plugins/graph-visualiser/api/graph" {
                // Not a graph, as when the service has stopped and something else answers.
                if CommandLine.arguments.contains("--verify-graph-down") { return Data("stopped".utf8) }
                return try encoder.encode(Self.graph)
            }
            if path.hasPrefix("/plugins/hub/api/") {
                return try hub(request, path: String(path.dropFirst("/plugins/hub/api/".count)))
            }
            if path.hasPrefix("/plugins/permissions/api/") {
                if request.httpMethod == "PUT" {
                    rules = try JSONDecoder().decode(PermissionRules.self, from: body(request))
                    rules.exposed = rules.rules.contains { $0.kind == "read" || $0.kind == "write" }
                        && !rules.rules.contains { $0.kind == "command" }
                }
                return try encoder.encode(rules)
            }
            switch (request.httpMethod, path) {
            case ("GET", "/plugins/market"):
                return try encoder.encode(market)
            case ("GET", "/plugins"):
                defer {
                    if let installing, let index = installed.firstIndex(where: { $0.name == installing }) {
                        installed[index].state = PluginState.ready
                        self.installing = nil
                    }
                }
                return try encoder.encode(PluginList(installed: installed, layout: layout))
            case ("POST", "/plugins"):
                let hub = Self.hub(state: PluginState.installing)
                installed.removeAll { $0.name == hub.name }
                installed.append(hub)
                installing = hub.name
                if !layout.switcher.contains(hub.name) { layout.switcher.append(hub.name) }
                return try encoder.encode(hub)
            case ("PUT", "/plugins/layout"):
                layout = try JSONDecoder().decode(PluginLayout.self, from: body(request))
                return try encoder.encode(layout)
            case ("PATCH", _):
                let patch = try JSONDecoder().decode(UpdatePlugin.self, from: body(request))
                guard let index = installed.firstIndex(where: { $0.name == name }) else { return Data() }
                installed[index].chats = patch.chats
                return try encoder.encode(installed[index])
            case ("DELETE", _):
                installed.removeAll { $0.name == name }
                layout.switcher.removeAll { $0 == name }
                return Data()
            default:
                return Data()
            }
        }
    }

    private func body(_ request: URLRequest) -> Data {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return Data() }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}
#endif
