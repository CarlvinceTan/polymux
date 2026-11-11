import SwiftUI
import GraphKit
import PolymuxKit

/// The Graph Visualiser: every bot's work, or one chat's. It only looks; nothing here starts or changes work.
struct GraphView: View {
    let plugin: Plugin
    /// The chat on show, or nil for everything.
    let chat: String?
    let session: AppSession
    var openChat: ((String) -> Void)?
    @State private var model: GraphModel
    @State private var pulses: [GraphPulse] = []
    @State private var previews = WorldPreviews()
    /// The same work as boxes and wires, for when the map isn't enough.
    @AppStorage("graphDiagram") private var diagram = false
    @Environment(\.scenePhase) private var scenePhase

    /// Beside a chat there's no room for details next to the graph, so they float over it.
    private var narrow: Bool { model.viewport.width < 640 }

    init(plugin: Plugin, chat: String?, session: AppSession, openChat: ((String) -> Void)? = nil) {
        self.plugin = plugin
        self.chat = chat
        self.session = session
        self.openChat = openChat
        _model = State(initialValue: GraphModel(key: chat ?? "global", mode: Self.opening(chat), cache: session.sidebar.cacheActor))
    }

    /// Everything at once only reads with room to spare.
    private static func opening(_ chat: String?) -> ViewMode {
        #if os(iOS)
        if UIDevice.current.userInterfaceIdiom == .phone { return .compact }
        #endif
        return chat == nil ? .full : .compact
    }

    var body: some View {
        Group {
            if diagram {
                canvas
            } else {
                WorldView(graph: model.graph, activity: session.sidebar.activity, loaded: model.loaded, previews: previews, openChat: openChat) { diagram = true }
            }
        }
        // Beside a chat the side panel's own surface shows through.
        .background(chat == nil ? Color.graphCanvas : Color.clear)
        .task(id: scenePhase == .background) { await follow() }
        .task(id: scenePhase == .background) { await followReports() }
        .onChange(of: session.sidebar.activity.handoffs) { _, handoffs in pulse(handoffs) }
        .onChange(of: session.work.activity) { show() }
        .onChange(of: session.work.reported) { show() }
        .onChange(of: session.work.settled) { show() }
        .onChange(of: session.sidebar.agents) { show() }
        .onChange(of: session.sidebar.threads) { show() }
    }

    private var canvas: some View {
        ZStack(alignment: .topLeading) {
            GeometryReader { geometry in
                GraphCanvas(model: model, activity: session.sidebar.activity, pulses: pulses, viewport: geometry.size)
                    .onChange(of: geometry.size, initial: true) { _, size in model.viewport = size }
            }
            if model.loaded && model.scene.nodes.isEmpty {
                Text("No activity yet.").foregroundStyle(.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            VStack(spacing: 8) {
                GraphToolButton(symbol: "map", label: "Show map") { diagram = false }
                    .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14))
                GraphToolbar(model: model, shortcuts: chat == nil)
            }
            .frame(maxHeight: .infinity)
            .padding(.leading, 12)
        }
        .overlay(alignment: narrow ? .bottom : .trailing) {
            if model.inspecting, model.selected.count == 1 || model.selectedWire != nil {
                inspector.transition(.opacity)
            }
        }
        .overlay(alignment: .top) {
            if model.selected.count > 1 { GraphSelectionBar(model: model).padding(.top, 12) }
        }
    }

    @ViewBuilder private var inspector: some View {
        let panel = GraphInspector(model: model, openChat: openChat)
        if narrow {
            panel.frame(maxWidth: .infinity, maxHeight: max(180, model.viewport.height * 0.42))
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 16))
                .padding(10)
        } else {
            panel.frame(width: 300).frame(maxHeight: .infinity)
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 16))
                .padding(10)
        }
    }

    private var pause: Duration { .seconds(scenePhase == .background ? 30 : 3) }

    /// Saved work first, then fresh work, for as long as the graph is on screen.
    private func follow() async {
        previews.load = { [client = session.client, name = plugin.name] id in try? await client.workPreview(plugin: name, node: id) }
        await model.restore()
        await session.work.restore()
        show()
        await session.network.refresh()
        show()
        while !Task.isCancelled {
            await session.work.refreshActivity()
            try? await Task.sleep(for: pause)
        }
    }

    /// Kept apart from `follow`, so a stuck plugin can't stop Polymux's own work updating.
    private func followReports() async {
        while !Task.isCancelled {
            await session.work.refreshReported(plugin: plugin.name)
            try? await Task.sleep(for: pause)
        }
    }

    private func show() {
        let builder = WorkGraphBuilder(agents: session.sidebar.agents, threads: session.sidebar.threads, activity: session.work.activity,
                                       reported: session.work.reported, network: session.network.value, chat: chat)
        model.show(builder.build(), settled: session.work.settled)
    }

    private func pulse(_ handoffs: [ActivityStore.Handoff]) {
        let now = Date()
        var next = pulses.filter { now.timeIntervalSince($0.at) < GraphPulse.duration }
        for handoff in handoffs where !next.contains(where: { $0.id == handoff.id }) {
            guard let (wire, reversed) = model.wire(from: WorkGraphBuilder.agentNode(handoff.source), to: WorkGraphBuilder.agentNode(handoff.target)) else { continue }
            next.append(GraphPulse(id: handoff.id, wire: wire, reversed: reversed, at: handoff.at, kind: handoff.result ? WorkKind.result : WorkKind.task))
        }
        pulses = next
    }
}

extension Color {
    /// Behind the graph, a step away from the nodes in both appearances.
    static var graphCanvas: Color {
        #if os(macOS)
        Color(nsColor: .underPageBackgroundColor)
        #else
        Color(uiColor: .systemGroupedBackground)
        #endif
    }
}
