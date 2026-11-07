import Foundation
import Observation

/// Editors receive metadata and blank replacement fields, never saved values.
@MainActor @Observable
public final class VaultLibrary {
    public private(set) var status: VaultStatus?
    public private(set) var items: [VaultSummary] = []
    public var draft: VaultItem?
    public private(set) var busy = false
    public private(set) var issue: String?
    public private(set) var loadIssue: String?
    public private(set) var imported: VaultImport?
    public private(set) var kdbx = false
    public private(set) var importName: String?
    public private(set) var keyName: String?
    private let client: VaultClient
    private var epoch = 0
    private var pending: Task<Void, Never>?
    private var kdbxFile: Data?
    private var keyFile: Data?

    public init(client: VaultClient) { self.client = client }
    public var hasKey: Bool { keyFile != nil }

    public func load() async {
        guard !busy else { return }
        let epoch = epoch
        do {
            let status = try await client.status()
            let items = try await client.items()
            guard self.epoch == epoch, !Task.isCancelled, !busy else { return }
            self.status = status; self.items = items; loadIssue = nil
        } catch {
            guard self.epoch == epoch, !Task.isCancelled else { return }
            loadIssue = error.isOffline ? nil : error.reason(or: "Couldn’t load the vault.")
        }
    }

    public func add(_ kind: String) {
        guard !busy else { return }
        close()
        imported = nil
        draft = VaultItem(id: UUID().uuidString, kind: kind)
    }

    public func open(_ id: String) {
        imported = nil
        perform { epoch in
            let item = try await self.client.item(id)
            guard self.epoch == epoch else { return }
            self.draft = item
        }
    }

    public func close() {
        epoch += 1
        pending?.cancel(); pending = nil
        draft = nil; busy = false; issue = nil
        kdbxFile = nil; keyFile = nil; importName = nil; keyName = nil; kdbx = false
    }

    public func importFile(_ url: URL) {
        guard !busy else { return }
        imported = nil
        perform { epoch in
            let data = try await VaultClient.readFile(url, limit: 32 << 20)
            guard self.epoch == epoch else { return }
            if VaultClient.isKDBX(data) || url.pathExtension.lowercased() == "kdbx" {
                self.kdbxFile = data
                self.importName = url.lastPathComponent
                self.kdbx = true
                self.keyFile = nil; self.keyName = nil
                return
            }
            guard data.count <= 8 << 20 else {
                throw ClientError.response(400, "Choose a CSV or JSON file up to 8 MiB, or a KDBX file up to 32 MiB.")
            }
            let result = try await self.client.importData(data)
            await self.finishImport(result, epoch: epoch)
        }
    }

    public func importKey(_ url: URL) {
        guard kdbx, !busy else { return }
        perform { epoch in
            let data = try await VaultClient.readFile(url, limit: 1 << 20)
            guard self.epoch == epoch else { return }
            self.keyFile = data
            self.keyName = url.lastPathComponent
        }
    }

    public func importKDBX(_ password: String) {
        guard let file = kdbxFile, !busy else { return }
        guard !password.isEmpty || hasKey else {
            issue = "Enter the KDBX password or choose a key file."
            return
        }
        let key = keyFile
        perform { epoch in
            let result = try await self.client.importKDBX(file: file, key: key, password: password)
            await self.finishImport(result, epoch: epoch)
        }
    }

    private func finishImport(_ result: VaultImport, epoch: Int) async {
        guard self.epoch == epoch else { return }
        let items = try? await client.items()
        guard self.epoch == epoch, !Task.isCancelled else { return }
        if let items { self.items = items }
        kdbxFile = nil; keyFile = nil; importName = nil; keyName = nil; kdbx = false
        imported = result
    }

    public func save() {
        guard let item = draft, !busy else { return }
        draft = nil
        let previous = items
        let summary = VaultSummary(id: item.id, kind: item.kind, title: item.title, origin: item.url,
                                   fields: Array(item.fields.keys).filter { !item.removed.contains($0) }, revision: item.revision)
        replace(summary)
        perform(rollback: { self.items = previous; self.draft = item }) { epoch in
            let saved = try await self.client.save(item)
            guard self.epoch == epoch else { return }
            self.replace(VaultSummary(id: saved.id, kind: saved.kind, title: saved.title, origin: saved.url,
                                      fields: Array(saved.fields.keys), revision: saved.revision))
        }
    }

    public func delete() {
        guard let item = draft, !busy else { return }
        let previous = items
        items.removeAll { $0.id == item.id }
        draft = nil
        perform(rollback: { self.items = previous; self.draft = item }) { _ in
            try await self.client.delete(item)
        }
    }

    private func replace(_ item: VaultSummary) {
        items.removeAll { $0.id == item.id }
        items.append(item)
        items.sort { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
    }

    private func perform(rollback: (@MainActor () -> Void)? = nil,
                         _ action: @escaping @MainActor (Int) async throws -> Void) {
        guard !busy else { return }
        busy = true; issue = nil
        epoch += 1
        let epoch = epoch
        pending = Task {
            do { try await action(epoch) }
            catch {
                guard self.epoch == epoch, !Task.isCancelled else { return }
                rollback?()
                self.issue = error.isOffline ? "You’re offline, so this wasn’t completed."
                    : error.reason(or: "Couldn’t complete this vault action.")
            }
            guard self.epoch == epoch else { return }
            self.busy = false; self.pending = nil
        }
    }
}
