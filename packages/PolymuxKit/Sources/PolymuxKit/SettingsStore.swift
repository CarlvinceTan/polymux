import Foundation
import Observation

@MainActor @Observable
public final class SettingsStore {
    private var remote: Settings?
    private var pending: [(UUID, SettingsPatch)] = []
    private var syncError: String?
    private var writeError: String?
    @ObservationIgnored private var retryPatch: SettingsPatch?
    public private(set) var isConnected = false
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private var writer: Task<Void, Never>?

    public init(client: APIClient) { self.client = client }

    public var value: Settings? {
        guard var value = remote else { return nil }
        for (_, patch) in pending { patch.apply(to: &value) }
        return value
    }

    public var isSaving: Bool { !pending.isEmpty }
    public var error: String? { writeError ?? syncError }

    public func refresh() async {
        do {
            accept(try await client.settings())
            isConnected = true
            syncError = nil
        } catch {
            if Task.isCancelled { return }
            isConnected = false
            syncError = error.localizedDescription
        }
    }

    public func edit(_ patch: SettingsPatch) {
        guard remote != nil else { return }
        pending.append((UUID(), patch))
        guard writer == nil else { return }
        writer = Task { [self] in
            try? await Task.sleep(for: .milliseconds(500))
            await flush()
        }
    }

    private func flush() async {
        defer { writer = nil }
        while !pending.isEmpty {
            let batch = pending
            var patch = SettingsPatch()
            for (_, update) in batch { patch.merge(update) }
            do {
                accept(try await client.updateSettings(patch))
                isConnected = true
                writeError = nil
                retryPatch = nil
            } catch {
                isConnected = false
                writeError = error.localizedDescription
                retryPatch = patch
            }
            let ids = Set(batch.map(\.0))
            pending.removeAll { ids.contains($0.0) }
        }
    }

    public func setKey(_ key: String?, provider: String) async throws {
        do {
            accept(try await client.speechKey(provider: provider, key: key))
            isConnected = true
            writeError = nil
        } catch {
            writeError = error.localizedDescription
            throw error
        }
    }

    public func retry() async {
        if let retryPatch {
            edit(retryPatch)
            while let writer { await writer.value }
        }
        await refresh()
        if isConnected, retryPatch == nil { writeError = nil }
    }

    public func synchronize() async -> Bool {
        while let writer { await writer.value }
        await refresh()
        return isConnected && writeError == nil && !isSaving
    }

    private func accept(_ value: Settings) {
        guard value.revision >= (remote?.revision ?? 0) else { return }
        remote = value
    }
}

extension SettingsPatch {
    func apply(to value: inout Settings) {
        if let appearance { value.appearance = appearance }
        if let sidebarWidth { value.sidebarWidth = sidebarWidth }
        if let voiceMode { value.voiceMode = voiceMode }
        if let liveProvider { value.liveProvider = liveProvider }
        if let liveModel { value.liveModel = liveModel }
        if let liveVoice { value.liveVoice = liveVoice }
        if let textProvider { value.textProvider = textProvider }
        if let textModel { value.textModel = textModel }
        if let voicePrompt { value.voicePrompt = voicePrompt }
    }

    mutating func merge(_ value: SettingsPatch) {
        appearance = value.appearance ?? appearance
        sidebarWidth = value.sidebarWidth ?? sidebarWidth
        voiceMode = value.voiceMode ?? voiceMode
        liveProvider = value.liveProvider ?? liveProvider
        liveModel = value.liveModel ?? liveModel
        liveVoice = value.liveVoice ?? liveVoice
        textProvider = value.textProvider ?? textProvider
        textModel = value.textModel ?? textModel
        voicePrompt = value.voicePrompt ?? voicePrompt
    }
}
