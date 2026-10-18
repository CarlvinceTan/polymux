import SwiftUI
import AvatarKit
import PolymuxKit

struct AvatarBlock: Codable, Equatable, Identifiable, Sendable {
    var id = UUID()
    var state: StateID
    var duration: Double

    init(state: StateID, duration: Double? = nil) {
        self.state = state
        self.duration = clampDuration(state, duration ?? STATE_BY_ID[state]?.duration ?? 2)
    }
}

struct AvatarOptions: Codable, Equatable, Sendable {
    var shape: ShapeID = .circle
    var color: ColorID = .ink
    var expression: ExpressionID = .neutral
    var followCursor = true
    var animates = true
    var playSequence = false
    var blocks: [AvatarBlock] = SEQUENCE.map { AvatarBlock(state: $0) }

    static let `default` = AvatarOptions()

    var duration: Double { blocks.reduce(0) { $0 + $1.duration } }

    init() {}

    init(design: AvatarDesign) {
        shape = ShapeID(rawValue: design.shape) ?? .circle
        color = ColorID(rawValue: design.color) ?? .ink
        expression = ExpressionID(rawValue: design.expression) ?? .neutral
        followCursor = design.followCursor
        animates = design.animates
        playSequence = design.playSequence
        blocks = design.blocks.prefix(200).compactMap { value in
            guard let state = StateID(rawValue: value.state) else { return nil }
            var block = AvatarBlock(state: state, duration: value.duration)
            block.id = UUID(uuidString: value.id) ?? UUID()
            return block
        }
        self = sanitized()
    }

    var design: AvatarDesign {
        let value = sanitized()
        return AvatarDesign(shape: value.shape.rawValue, color: value.color.rawValue,
                            expression: value.expression.rawValue, followCursor: value.followCursor,
                            animates: value.animates, playSequence: value.playSequence,
                            blocks: value.blocks.map {
                                AnimationBlock(id: $0.id.uuidString, state: $0.state.rawValue, duration: $0.duration)
                            })
    }

    func sanitized() -> AvatarOptions {
        var result = self
        result.color = color.avatarID
        result.blocks = Array(blocks.prefix(200)).map { block in
            var copy = block
            copy.duration = clampDuration(copy.state, copy.duration.isFinite ? copy.duration : 2)
            return copy
        }
        return result
    }
}

@MainActor @Observable final class AvatarStore {
    let trace = SignpostTrace()
    private(set) var saved: [String: AvatarOptions] = [:]
    private(set) var dirtyIDs: Set<String> = []
    @ObservationIgnored private let sidebar: SidebarStore
    @ObservationIgnored private var editing: [String: Set<UUID>] = [:]
    @ObservationIgnored private var writes: [String: AvatarWrite] = [:]
    @ObservationIgnored private var knownIDs: Set<String>?
    @ObservationIgnored private var flushing = false
    @ObservationIgnored private var removedIDs: Set<String> = []

    private struct AvatarWrite {
        let token = UUID()
        let options: AvatarOptions
        let task: Task<Agent, any Error>
    }

    init(sidebar: SidebarStore) {
        self.sidebar = sidebar
        sync(agents: sidebar.agents)
    }

    func options(for agentID: String) -> AvatarOptions {
        saved[agentID] ?? .default
    }

    func set(_ options: AvatarOptions, for agentID: String) {
        guard !agentID.isEmpty, !removedIDs.contains(agentID) else { return }
        let value = options.sanitized()
        guard saved[agentID] != value else { return }
        saved[agentID] = value
        dirtyIDs.insert(agentID)
    }

    func reset(_ agentID: String) {
        set(.default, for: agentID)
    }

    func remove(id: String) {
        removedIDs.insert(id)
        writes.removeValue(forKey: id)?.task.cancel()
        saved.removeValue(forKey: id)
        dirtyIDs.remove(id)
        editing.removeValue(forKey: id)
        knownIDs?.remove(id)
    }

    func beginEditing(_ agentID: String, token: UUID) {
        editing[agentID, default: []].insert(token)
    }

    func endEditing(_ agentID: String, token: UUID) {
        editing[agentID]?.remove(token)
        if editing[agentID]?.isEmpty == true { editing.removeValue(forKey: agentID) }
    }

    func sync(agents: [Agent]) {
        knownIDs = Set(agents.map(\.id))
        for id in saved.keys where knownIDs?.contains(id) == false { remove(id: id) }
        for agent in agents {
            guard !removedIDs.contains(agent.id), !dirtyIDs.contains(agent.id), editing[agent.id] == nil else { continue }
            if let design = agent.avatar {
                let value = AvatarOptions(design: design)
                if saved[agent.id] != value { saved[agent.id] = value }
            } else {
                saved.removeValue(forKey: agent.id)
            }
        }
    }

    func finish(_ agentID: String, previous: AvatarOptions, client: APIClient) {
        let request = options(for: agentID).design
        dirtyIDs.remove(agentID)
        sidebar.commits.saveAvatar(id: agentID, previous: previous.design, request: request,
                                   sidebar: sidebar, client: client, apply: { [self] design in
            guard !removedIDs.contains(agentID) else { return }
            saved[agentID] = AvatarOptions(design: design)
        }, close: {})
    }

    func flush(client: APIClient) async {
        guard !flushing else { return }
        flushing = true
        defer { flushing = false }
        for agentID in dirtyIDs.sorted() {
            guard editing[agentID] == nil, knownIDs?.contains(agentID) != false else { continue }
            do { try await commit(for: agentID, client: client, allowEditing: false) }
            catch {
                sidebar.commits.report("Couldn’t save the avatar. Your changes remain on this device.") { [self] in
                    Task { await flush(client: client) }
                }
            }
        }
    }

    func commit(for agentID: String, client: APIClient, allowEditing: Bool = true) async throws {
        while dirtyIDs.contains(agentID) {
            if !allowEditing, editing[agentID] != nil { return }
            try Task.checkCancellation()
            let write: AvatarWrite
            if let pending = writes[agentID] {
                write = pending
            } else {
                let options = options(for: agentID)
                let design = options.design
                write = AvatarWrite(options: options, task: Task {
                    try await client.updateAvatar(id: agentID, request: design)
                })
                writes[agentID] = write
            }
            do {
                let agent = try await write.task.value
                let current = writes[agentID]?.token == write.token
                if current { writes.removeValue(forKey: agentID) }
                if current, !removedIDs.contains(agentID) { await sidebar.accept(agent) }
                if current, !removedIDs.contains(agentID), options(for: agentID) == write.options {
                    if let design = agent.avatar { saved[agentID] = AvatarOptions(design: design) }
                    dirtyIDs.remove(agentID)
                }
            } catch {
                if writes[agentID]?.token == write.token { writes.removeValue(forKey: agentID) }
                throw error
            }
        }
    }

}

extension Color {
    init(avatarHex: String) {
        let hex = avatarHex.trimmingCharacters(in: CharacterSet(charactersIn: "#"))
        let value = UInt64(hex, radix: 16) ?? 0x0a0a0c
        self.init(.sRGB, red: Double((value >> 16) & 255) / 255,
                  green: Double((value >> 8) & 255) / 255,
                  blue: Double(value & 255) / 255, opacity: 1)
    }
}
