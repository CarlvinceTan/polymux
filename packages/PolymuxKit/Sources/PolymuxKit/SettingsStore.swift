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
    @ObservationIgnored private let cache: LocalCacheActor?
    @ObservationIgnored private var writer: Task<Void, Never>?
    @ObservationIgnored private var resend: Task<Void, Never>?

    /// With a cache, the last settings seen show straight away and while offline.
    public init(client: APIClient, cache: LocalCacheActor? = nil) {
        self.client = client
        self.cache = cache
    }

    /// Shows the cached settings until the server answers.
    public func restore() async {
        guard remote == nil, let cached: Settings = await cache?.snapshot("settings"), remote == nil else { return }
        remote = cached
    }

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
            syncError = error.isOffline ? nil : "Couldn’t load settings."
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
            // A change still waiting to retry rides along, so a later write can't drop it.
            var patch = retryPatch ?? SettingsPatch()
            for (_, update) in batch { patch.merge(update) }
            do {
                accept(try await client.updateSettings(patch))
                isConnected = true
                writeError = nil
                retryPatch = nil
            } catch {
                isConnected = false
                // Only a lost connection retries; a change the server refused would fail again.
                writeError = error.isTransient ? "Not saved yet. It will try again." : error.reason(or: "Couldn’t save that setting.")
                if error.isTransient { retryPatch = patch }
                if error.isTransient { scheduleRetry() }
            }
            let ids = Set(batch.map(\.0))
            pending.removeAll { ids.contains($0.0) }
        }
    }

    /// Sends a change that couldn’t get through again after a pause, until it does.
    private func scheduleRetry() {
        guard resend == nil else { return }
        resend = Task { [weak self] in
            try? await Task.sleep(for: .seconds(5))
            guard let self, !Task.isCancelled else { return }
            self.resend = nil
            if self.retryPatch != nil { await self.retry() }
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
        if let cache { Task { await cache.saveSnapshot("settings", value) } }
    }
}

extension SettingsPatch {
    /// Overlays this patch's set fields on the value. Going through JSON covers every field the contract defines.
    func apply(to value: inout Settings) {
        if let applied: Settings = try? Self.overlay(value, self) { value = applied }
    }

    /// Later edits win field by field.
    mutating func merge(_ value: SettingsPatch) {
        if let merged: SettingsPatch = try? Self.overlay(self, value) { self = merged }
    }

    private static func overlay<Base: Codable>(_ base: Base, _ patch: SettingsPatch) throws -> Base {
        var fields = try JSONSerialization.jsonObject(with: JSONEncoder().encode(base)) as? [String: Any] ?? [:]
        let changes = try JSONSerialization.jsonObject(with: JSONEncoder().encode(patch)) as? [String: Any] ?? [:]
        fields.merge(changes) { _, new in new }
        return try JSONDecoder().decode(Base.self, from: JSONSerialization.data(withJSONObject: fields))
    }
}
