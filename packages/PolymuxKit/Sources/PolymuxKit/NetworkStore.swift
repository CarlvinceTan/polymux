import Foundation
import Observation

@MainActor @Observable
public final class NetworkStore {
    public private(set) var value: NetworkInfo?
    public private(set) var isConnected = false
    public private(set) var hasChecked = false
    public private(set) var issue: String?
    public private(set) var actionIssue: String?
    /// True while this server refuses this device's credential.
    public private(set) var denied = false
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var loading: Task<Void, Never>?
    @ObservationIgnored private var checkedAt: ContinuousClock.Instant?
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var pending: Set<String> = []

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    public func restore() async {
        if let saved: NetworkInfo = await cache.snapshot("network"), value == nil { value = saved }
    }

    public func refresh() async {
        if let loading { await loading.value; return }
        guard pending.isEmpty else { return }
        if let checkedAt, checkedAt.duration(to: .now) < .seconds(2) { return }
        let revision = revision
        let task = Task {
            do {
                let fresh = try await client.network()
                isConnected = true
                hasChecked = true
                issue = nil
                denied = false
                guard self.revision == revision, pending.isEmpty else { return }
                value = fresh
                checkedAt = .now
                await cache.saveSnapshot("network", fresh)
            } catch {
                isConnected = false
                hasChecked = true
                issue = error.isOffline ? nil : "Couldn’t load devices."
                if case ClientError.response(401, _) = error { denied = true }
            }
        }
        loading = task
        await task.value
        loading = nil
    }

    public func name(_ network: String) async {
        let before = value?.server.network
        guard network != before, pending.insert("name").inserted else { return }
        revision += 1
        value?.server.network = network
        actionIssue = nil
        do { try await client.nameNetwork(network) }
        catch {
            value?.server.network = before
            actionIssue = "Couldn’t rename this network."
        }
        pending.remove("name")
        revision += 1
        await persist()
    }

    public func rename(_ device: NetworkInfo.Device, label: String) async {
        guard let index = value?.devices.firstIndex(where: { $0.id == device.id }),
              pending.insert(device.id).inserted else { return }
        revision += 1
        value?.devices[index].label = label
        actionIssue = nil
        do { try await client.labelDevice(device.id, label: label) }
        catch {
            if let index = value?.devices.firstIndex(where: { $0.id == device.id }) { value?.devices[index].label = device.label }
            actionIssue = "Couldn’t rename \(device.title)."
        }
        pending.remove(device.id)
        revision += 1
        await persist()
    }

    public func remove(_ device: NetworkInfo.Device) async {
        guard let index = value?.devices.firstIndex(where: { $0.id == device.id }),
              pending.insert(device.id).inserted else { return }
        revision += 1
        value?.devices.remove(at: index)
        actionIssue = nil
        do { try await client.removeDevice(device.id) }
        catch {
            let insertion = min(index, value?.devices.count ?? 0)
            value?.devices.insert(device, at: insertion)
            actionIssue = "Couldn’t remove \(device.title)."
        }
        pending.remove(device.id)
        revision += 1
        await persist()
    }

    public func allow(_ device: NetworkInfo.Device) async {
        guard let index = value?.restricted?.firstIndex(where: { $0.id == device.id }),
              pending.insert(device.id).inserted else { return }
        revision += 1
        value?.restricted?.remove(at: index)
        value?.devices.append(device)
        actionIssue = nil
        do { try await client.allowDevice(device.id) }
        catch {
            value?.devices.removeAll { $0.id == device.id }
            value?.restricted?.insert(device, at: min(index, value?.restricted?.count ?? 0))
            actionIssue = "Couldn’t allow \(device.title)."
        }
        pending.remove(device.id)
        revision += 1
        await persist()
    }

    private func persist() async {
        if let value { await cache.saveSnapshot("network", value) }
    }
}
