import Foundation
import Observation

@MainActor @Observable
public final class ProviderStore {
    public private(set) var providers: [Provider]?
    public private(set) var options: [ProviderOption]?
    public private(set) var issue: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var restored = false
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var pending = 0
    @ObservationIgnored private var loading: Task<Void, Never>?
    @ObservationIgnored private var catalog: Task<Void, Never>?
    @ObservationIgnored private var checkedAt: ContinuousClock.Instant?
    @ObservationIgnored private var catalogAt: ContinuousClock.Instant?

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    public var names: [String: String] {
        Dictionary((options ?? []).map { ($0.type, $0.name) }, uniquingKeysWith: { first, _ in first })
    }

    public func restore() async {
        guard !restored else { return }
        restored = true
        if let saved: [Provider] = await cache.snapshot("providers"), providers == nil { providers = saved }
        if let saved: [ProviderOption] = await cache.snapshot("provider-options"), options == nil { options = saved }
    }

    public func refresh() async {
        async let list: () = refreshProviders()
        async let options: () = refreshOptions()
        _ = await (list, options)
    }

    public func refreshProviders(force: Bool = false) async {
        if let loading { await loading.value; return }
        guard pending == 0 else { return }
        if !force, let checkedAt, checkedAt.duration(to: .now) < .seconds(2) { return }
        let revision = revision
        let task = Task {
            do {
                let list = try await client.providers()
                guard self.revision == revision, pending == 0 else { return }
                issue = nil
                checkedAt = .now
                if providers != list { providers = list; persist() }
            } catch {
                if !error.isOffline { issue = "Couldn’t load providers." }
            }
        }
        loading = task
        await task.value
        loading = nil
    }

    public func refreshOptions() async {
        if let catalog { await catalog.value; return }
        if let catalogAt, catalogAt.duration(to: .now) < .seconds(60) { return }
        let task = Task {
            guard let list = try? await client.providerOptions() else { return }
            catalogAt = .now
            if options != list {
                options = list
                await cache.saveSnapshot("provider-options", list)
            }
        }
        catalog = task
        await task.value
        catalog = nil
    }

    public func accept(_ list: [Provider]) {
        revision += 1
        providers = list
        issue = nil
        persist()
    }

    public func merge(_ updates: [Provider]) {
        var list = providers ?? []
        for provider in updates {
            if let index = list.firstIndex(where: { $0.id == provider.id }) { list[index] = provider }
            else { list.append(provider) }
        }
        accept(list)
    }

    public func remove(_ id: String) { accept((providers ?? []).filter { $0.id != id }) }

    public func begin() { pending += 1; revision += 1 }
    public func end() { pending -= 1; revision += 1 }

    private func persist() {
        guard let providers else { return }
        // Provider headers can contain credentials; only public metadata belongs in this cache.
        let saved = providers.map { provider in
            var copy = provider
            copy.headers = nil
            return copy
        }
        Task { await cache.saveSnapshot("providers", saved) }
    }
}
