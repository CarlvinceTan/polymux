import Foundation
import Observation

/// Private cards edit replacements without receiving saved secret values.
@MainActor @Observable
public final class VaultStore {
    public private(set) var status: VaultStatus?
    public var draft: VaultItem?
    public private(set) var busy = false
    public private(set) var issue: String?
    public private(set) var done = false
    public private(set) var imported: VaultImport?
    public private(set) var kdbx: Bool
    public private(set) var importName: String?
    public private(set) var keyName: String?
    private let client: VaultClient
    private let card: VaultCard
    private let messageID: String
    private var kdbxFile: Data?
    private var keyFile: Data?
    private var epoch = 0
    private var pending: Task<Void, Never>?

    public init(client: VaultClient, card: VaultCard, messageID: String) {
        self.client = client; self.card = card; self.messageID = messageID
        kdbx = !(card.path ?? "").isEmpty
        importName = Self.fileName(card.path)
        keyName = Self.fileName(card.keyPath)
    }
    public var serverFile: Bool { !(card.path ?? "").isEmpty }
    public var hasKey: Bool { keyFile != nil || !(card.keyPath ?? "").isEmpty }

    public func load() {
        perform { epoch in
            let status = try await self.client.status()
            guard self.epoch == epoch else { return }
            self.status = status
            if self.card.action == VaultCardAction.edit {
                let item: VaultItem
                do { item = try await self.client.item(self.card.itemID) }
                catch ClientError.response(404, _) {
                    item = VaultItem(id: self.card.itemID, kind: self.card.kind, title: self.card.title, url: self.card.url)
                }
                guard self.epoch == epoch else { return }
                self.draft = item
            }
        }
    }

    public func close() {
        epoch += 1
        pending?.cancel(); pending = nil
        draft = nil; status = nil; busy = false; issue = nil
        kdbxFile = nil; keyFile = nil
        kdbx = serverFile
        importName = Self.fileName(card.path)
        keyName = Self.fileName(card.keyPath)
    }

    public func save() {
        guard let item = draft, !busy else { return }
        draft = nil
        perform(rollback: { self.draft = item }) { epoch in
            _ = try await self.client.saveCard(self.messageID, item: item)
            guard self.epoch == epoch else { return }
            self.done = true
            self.close()
        }
    }

    public func importFile(_ url: URL) {
        guard !busy else { return }
        perform { epoch in
            let data = try await VaultClient.readFile(url, limit: 32 << 20)
            guard self.epoch == epoch else { return }
            if VaultClient.isKDBX(data) || url.pathExtension.lowercased() == "kdbx" {
                self.kdbxFile = data
                self.kdbx = true
                self.importName = url.lastPathComponent
                self.keyFile = nil
                self.keyName = Self.fileName(self.card.keyPath)
                return
            }
            guard data.count <= 8 << 20 else {
                throw ClientError.response(400, "Choose a CSV or JSON file up to 8 MiB, or a KDBX file up to 32 MiB.")
            }
            self.kdbxFile = nil; self.keyFile = nil
            self.kdbx = self.serverFile
            self.importName = Self.fileName(self.card.path)
            self.keyName = Self.fileName(self.card.keyPath)
            let result = try await self.client.importData(data, card: self.messageID)
            guard self.epoch == epoch else { return }
            self.imported = result
            self.done = true
            self.close()
        }
    }

    public func importKeyFile(_ url: URL) {
        guard kdbx, !busy else { return }
        perform { epoch in
            let data = try await VaultClient.readFile(url, limit: 1 << 20)
            guard self.epoch == epoch else { return }
            self.keyFile = data
            self.keyName = url.lastPathComponent
        }
    }

    public func importKDBX(password: String) {
        guard kdbx, kdbxFile != nil || serverFile, !busy else { return }
        guard !password.isEmpty || hasKey else {
            issue = "Enter the KDBX master password or choose a key file."
            return
        }
        let file = kdbxFile, key = keyFile
        perform { epoch in
            let result = try await self.client.importKDBX(file: file, key: key, password: password,
                                                          card: self.messageID)
            guard self.epoch == epoch else { return }
            self.imported = result
            self.done = true
            self.close()
        }
    }

    private static func fileName(_ path: String?) -> String? {
        guard let path, !path.isEmpty else { return nil }
        let name = URL(fileURLWithPath: path).lastPathComponent
        return name.isEmpty ? nil : name
    }

    private func perform(rollback: (@MainActor () -> Void)? = nil,
                         _ action: @escaping @MainActor (Int) async throws -> Void) {
        guard !busy else { return }
        busy = true; issue = nil
        let epoch = epoch
        pending = Task {
            do { try await action(epoch) }
            catch {
                guard self.epoch == epoch, !Task.isCancelled else { return }
                rollback?()
                self.issue = error.isOffline ? "You’re offline, so this wasn’t completed."
                    : error.reason(or: "Couldn’t complete this private vault action.")
            }
            guard self.epoch == epoch else { return }
            self.busy = false; self.pending = nil
        }
    }
}
