import SwiftUI
import AvatarKit
import PolymuxKit

struct AvatarEditor: View {
    let agentID: String
    let name: String
    let client: APIClient?
    @Bindable var store: AvatarStore
    @Environment(\.panelClose) private var dismiss
    @Environment(\.scenePhase) private var scene
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.colorScheme) private var scheme
    @State private var preview: StateID = .idle
    @State private var playing = true
    @State private var sequence = false
    @State private var cursor = 0.0
    @State private var blocks: [AvatarBlock]
    @State private var advanced = false
    @State private var previewDark: Bool?
    @State private var saving = false
    @State private var issue: String?
    @State private var editID = UUID()

    init(agentID: String, name: String, client: APIClient? = nil, store: AvatarStore) {
        self.agentID = agentID
        self.name = name
        self.client = client
        self.store = store
        _blocks = State(initialValue: store.options(for: agentID).blocks)
    }

    private var options: AvatarOptions { store.options(for: agentID) }
    private var duration: Double {
        sequence ? max(0.1, blocks.reduce(0) { $0 + $1.duration }) : STATE_BY_ID[preview]?.duration ?? 2
    }
    private var ticking: Bool { advanced && playing && options.animates && !reduceMotion && scene == .active }
    private var columns: [GridItem] { [GridItem(.adaptive(minimum: 76), spacing: 8)] }

    var body: some View {
        PanelView("\(name)’s avatar", enabled: !saving, onClose: { dismiss() }) {
            Form {
                Section {
                    PillPicker("Options", selection: $advanced, options: [
                        ("Basic", false), ("Advanced", true)
                    ])
                    .frame(width: 208)
                }
                .listRowSeparator(.hidden)
                .panelRow()
                Section {
                    previewPane.frame(maxWidth: .infinity)
                }
                .listRowSeparator(.hidden)
                .panelRow()
                if advanced {
                    Section { previewControls }
                        .listRowSeparator(.hidden)
                        .panelRow()
                }
                Section("Shape") { shapePicker }
                    .panelRow()
                Section("Colour") { colorPicker }
                    .panelRow()
                if advanced {
                    Section("Resting expression") { expressionPicker }
                        .panelRow()
                    Section("Animation") {
                        Picker("Preview", selection: $preview) {
                            ForEach(StateID.allCases) { state in Text(state.label).tag(state) }
                        }
                        .disabled(sequence)
                        Toggle("Animate avatar", isOn: binding(\.animates))
                        Toggle("Follow cursor", isOn: binding(\.followCursor))
                        Toggle("Use sequence in sidebar", isOn: binding(\.playSequence))
                    }
                    .panelRow()
                    Section("Sequence") {
                        Toggle("Preview sequence", isOn: Binding(get: { sequence }, set: { sequence = $0; cursor = 0 }))
                            .disabled(blocks.isEmpty)
                        ForEach(Array(blocks.enumerated()), id: \.element.id) { index, block in
                            blockRow(index, block)
                        }
                        HStack {
                            Menu("Add animation", systemImage: "plus") {
                                ForEach(StateID.allCases) { state in
                                    Button(state.label) { blocks.append(AvatarBlock(state: state)) }
                                }
                            }
                            .disabled(blocks.count >= 200)
                            Spacer()
                            Button("Save sequence") { saveBlocks() }
                                .disabled(blocks == options.blocks)
                        }
                        Button("Restore original sequence") {
                            blocks = SEQUENCE.map { AvatarBlock(state: $0) }
                            cursor = 0
                        }
                    }
                    .panelRow()
                    Section {
                        Button("Reset avatar") {
                            store.reset(agentID)
                            blocks = options.blocks
                            preview = .idle
                            sequence = false
                            cursor = 0
                            playing = true
                        }
                    }
                    .panelRow()
                }
                if let issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled) }
                        .panelRow()
                }
            }
            .formStyle(.grouped)
            .scrollIndicators(.hidden)
            .disabled(saving)
        } actions: {
            Button { Task { await finish() } } label: {
                if saving { ProgressView().controlSize(.small) }
                else { Text("Done") }
            }
            .disabled(saving)
        }
        .interactiveDismissDisabled(saving)
        .panelDismiss(enabled: !saving, onClose: { dismiss() })
        .onAppear { store.beginEditing(agentID, token: editID) }
        .onDisappear {
            saveBlocks()
            store.endEditing(agentID, token: editID)
            if let client { Task { await store.flush(client: client) } }
        }
        .onChange(of: preview) { _, _ in cursor = 0 }
        .onChange(of: duration) { _, value in cursor = min(cursor, value) }
        .task(id: ticking) {
            guard ticking else { return }
            var last = Date()
            while !Task.isCancelled {
                do { try await Task.sleep(for: .seconds(1 / 30.0)) }
                catch { return }
                let now = Date()
                let delta = max(0, min(0.064, now.timeIntervalSince(last)))
                last = now
                cursor = (cursor + delta).truncatingRemainder(dividingBy: duration)
            }
        }
    }

    private var previewPane: some View {
        Group {
            if advanced {
                AvatarView(options: options, state: preview, playing: false,
                           paper: previewPaper, timeline: sequence ? blocks : nil, time: cursor)
                    .frame(width: 200, height: 200)
            } else {
                AvatarView(options: options, paper: previewPaper).frame(width: 200, height: 200)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity)
        .background(previewPaper, in: RoundedRectangle(cornerRadius: 20))
        .overlay(alignment: .topTrailing) {
            Button {
                previewDark = previewScheme != .dark
            } label: {
                Image(systemName: previewScheme == .dark ? "moon.fill" : "sun.max.fill")
                    .font(.system(size: 14))
                    .frame(width: 36, height: 36)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: Circle())
            .accessibilityLabel(previewScheme == .dark ? "Preview in light mode" : "Preview in dark mode")
            .accessibilityValue(previewScheme == .dark ? "Dark preview" : "Light preview")
            .help("Change the avatar preview appearance")
            .padding(12)
        }
        .environment(\.colorScheme, previewScheme)
    }

    private var previewScheme: ColorScheme {
        (previewDark ?? (scheme == .dark)) ? .dark : .light
    }

    private var previewPaper: Color {
        #if os(macOS)
        Color(nsColor: .windowBackgroundColor)
        #else
        Color(uiColor: .systemBackground)
        #endif
    }

    private var previewControls: some View {
        VStack(spacing: 6) {
            HStack(spacing: 12) {
                Button(playing ? "Pause" : "Play", systemImage: playing ? "pause.fill" : "play.fill") {
                    playing.toggle()
                }
                .labelStyle(.iconOnly)
                .disabled(!options.animates || reduceMotion)
                Slider(value: $cursor, in: 0...duration, onEditingChanged: { editing in
                    if editing { playing = false }
                })
                .accessibilityLabel("Animation position")
                Text(cursor, format: .number.precision(.fractionLength(1)))
                    .font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    .frame(width: 36, alignment: .trailing)
            }
            Text(sequence ? "Sequence" : preview.label)
                .font(.caption).foregroundStyle(.secondary)
        }
    }

    private var shapePicker: some View {
        LazyVGrid(columns: columns, spacing: 8) {
            ForEach(ShapeID.allCases) { shape in
                var choice = options
                let _ = choice.shape = shape
                tile(choice, label: shape.label, selected: options.shape == shape) {
                    set(\.shape, shape)
                }
            }
        }
    }

    private var expressionPicker: some View {
        LazyVGrid(columns: columns, spacing: 8) {
            ForEach(ExpressionID.allCases) { expression in
                var choice = options
                let _ = choice.expression = expression
                tile(choice, label: expression.label, selected: options.expression == expression) {
                    set(\.expression, expression)
                }
            }
        }
    }

    private var colorPicker: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 40), spacing: 12)], spacing: 12) {
            ForEach(ColorID.avatarColors) { color in
                Button { set(\.color, color) } label: {
                    AvatarSwatch(color: color)
                        .frame(width: 32, height: 32)
                        .overlay { Circle().stroke(.primary.opacity(0.15), lineWidth: 1) }
                        .padding(4)
                        .overlay {
                            Circle().stroke(options.color.avatarID == color ? Color.primary : .clear, lineWidth: 2)
                        }
                }
                .buttonStyle(.plain)
                .accessibilityLabel(color.avatarLabel)
                .accessibilityValue(options.color.avatarID == color ? "Selected" : "")
                .help(color.avatarLabel)
            }
        }
    }

    private func tile(_ choice: AvatarOptions, label: String, selected: Bool,
                      action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 0) {
                AvatarView(options: choice, playing: false, frozenAt: 1)
                    .frame(width: 58, height: 58)
                Text(label).font(.caption2).lineLimit(1).foregroundStyle(.primary)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 4)
            .background(selected ? Color.primary.opacity(0.07) : .clear, in: RoundedRectangle(cornerRadius: 10))
            .overlay { RoundedRectangle(cornerRadius: 10).stroke(selected ? Color.primary.opacity(0.45) : .clear) }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityValue(selected ? "Selected" : "")
    }

    private func blockRow(_ index: Int, _ block: AvatarBlock) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Button { seek(index) } label: {
                    AvatarView(options: options, state: block.state, playing: false, frozenAt: 1)
                        .frame(width: 44, height: 44)
                }
                .buttonStyle(.plain).accessibilityLabel("Preview \(block.state.label)")
                Picker("Animation \(index + 1)", selection: blockBinding(block, \.state)) {
                    ForEach(StateID.allCases) { state in Text(state.label).tag(state) }
                }
                .labelsHidden()
                Spacer(minLength: 4)
                Button { move(index, -1) } label: { Image(systemName: "arrow.up") }
                    .disabled(index == 0).accessibilityLabel("Move animation earlier")
                Button { move(index, 1) } label: { Image(systemName: "arrow.down") }
                    .disabled(index == blocks.count - 1).accessibilityLabel("Move animation later")
                Button(role: .destructive) {
                    blocks.removeAll { $0.id == block.id }
                    if blocks.isEmpty { sequence = false }
                } label: { Image(systemName: "minus.circle") }
                .accessibilityLabel("Remove \(block.state.label)")
            }
            HStack {
                Text("Hold").font(.caption).foregroundStyle(.secondary)
                Slider(value: blockBinding(block, \.duration), in: minDurationOf(block.state)...10, step: 0.1)
                    .accessibilityLabel("Hold duration for \(block.state.label)")
                Text("\(block.duration, specifier: "%.1f") s")
                    .font(.caption.monospacedDigit()).frame(width: 44, alignment: .trailing)
            }
        }
    }

    private func binding<Value>(_ keyPath: WritableKeyPath<AvatarOptions, Value>) -> Binding<Value> {
        Binding(get: { options[keyPath: keyPath] }, set: { set(keyPath, $0) })
    }

    private func set<Value>(_ keyPath: WritableKeyPath<AvatarOptions, Value>, _ value: Value) {
        var next = options
        next[keyPath: keyPath] = value
        store.set(next, for: agentID)
    }

    private func blockBinding<Value>(_ block: AvatarBlock, _ keyPath: WritableKeyPath<AvatarBlock, Value>) -> Binding<Value> {
        Binding(get: { blocks.first(where: { $0.id == block.id })?[keyPath: keyPath] ?? block[keyPath: keyPath] }, set: { value in
            guard let index = blocks.firstIndex(where: { $0.id == block.id }) else { return }
            blocks[index][keyPath: keyPath] = value
            blocks[index].duration = clampDuration(blocks[index].state, blocks[index].duration)
        })
    }

    private func move(_ index: Int, _ delta: Int) {
        let target = index + delta
        guard blocks.indices.contains(target) else { return }
        blocks.swapAt(index, target)
        cursor = 0
    }

    private func seek(_ index: Int) {
        playing = false
        sequence = true
        cursor = blocks.prefix(index).reduce(0) { $0 + $1.duration }
    }

    private func finish() async {
        guard !saving else { return }
        saveBlocks()
        guard let client else { dismiss(); return }
        saving = true
        issue = nil
        defer { saving = false }
        do {
            try await store.commit(for: agentID, client: client)
            dismiss()
        } catch {
            issue = "Couldn’t save your avatar to the server. Your changes are still open here. \(error.localizedDescription)"
        }
    }

    private func saveBlocks() {
        var next = options
        next.blocks = blocks
        store.set(next, for: agentID)
    }
}
