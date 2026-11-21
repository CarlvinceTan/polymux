#if DEBUG
import Foundation
import PolymuxKit

/// A pretend server's mail, for `--verify-hub-mail`: two mailboxes that hold what the app does to them.
final class HubMailFixture: @unchecked Sendable {
    static var enabled: Bool { CommandLine.arguments.contains("--verify-hub-mail") }

    /// Opens a screen straight away, for a device that can't be driven: `--verify-hub-mail-view list|reader|compose`.
    @MainActor static func show(_ store: HubStore) async {
        guard let flag = CommandLine.arguments.firstIndex(of: "--verify-hub-mail-view"), flag + 1 < CommandLine.arguments.count else { return }
        while store.mail.accounts.isEmpty, !Task.isCancelled { try? await Task.sleep(for: .milliseconds(200)) }
        store.source = .mail
        await store.mail.select("mail-1")
        switch CommandLine.arguments[flag + 1] {
        case "reader": if let first = store.mail.envelopes.first { await store.mail.read(first) }
        case "reader-html": if let row = store.mail.envelopes.first(where: { $0.id == 3 }) { await store.mail.read(row) }
        case "reader-plain": if let row = store.mail.envelopes.first(where: { $0.id == 4 }) { await store.mail.read(row) }
        case "compose": await store.mail.start()
        default: break
        }
    }

    private static let png = Data(base64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")!
    private static let signature = HubMailSignature(id: "sig-work", name: "Work", text: "Carl\nPolymux", html: "<b>Carl</b><br>Polymux")
    private var signatures = ["mail-1": [signature], "mail-2": []]
    private var defaults = ["mail-1": "sig-work"]
    private var names = ["mail-1": "Me", "mail-2": "Work"]
    private var mail: [String: [String: [HubMailEnvelope]]]
    private var nextID: Int64 = 100
    private var uploads: [String: HubMailUpload] = [:]
    private var bodies: [Int64: HubMailSend] = [:]

    private static func envelope(_ id: Int64, _ account: String, _ folder: String, from: String, subject: String, preview: String, at: Int64,
                                 seen: Bool = true, flagged: Bool = false, files: Int64 = 0, answered: Bool = false,
                                 importance: String = HubMailEnvelopeImportance.normal) -> HubMailEnvelope {
        HubMailEnvelope(id: id, account: account, folder: folder, from: from, address: from.lowercased().replacingOccurrences(of: " ", with: ".") + "@example.test",
                        subject: subject, preview: preview, at: at, seen: seen, flagged: flagged, attachments: files, answered: answered, draft: false, importance: importance)
    }

    /// More than a page, to prove the list keeps loading.
    private static func archive(now: Int64) -> [HubMailEnvelope] {
        (0..<60).map { index in envelope(Int64(1000 + index), "mail-1", "Archive", from: "Archive", subject: "Old letter \(index)", preview: "", at: now - Int64(index) * 60_000) }
    }

    init() {
        let now = Int64(Date.now.timeIntervalSince1970 * 1000)
        let hour: Int64 = 3_600_000
        mail = ["mail-1": ["INBOX": [
            Self.envelope(1, "mail-1", "INBOX", from: "Priya Raman", subject: "Q3 numbers", preview: "The figures are attached.", at: now - hour, seen: false, files: 2),
            Self.envelope(2, "mail-1", "INBOX", from: "Sam Lee", subject: "Re: Q3 numbers", preview: "Looks right to me.", at: now - 2 * hour, answered: true),
            Self.envelope(3, "mail-1", "INBOX", from: "Billing", subject: "Invoice 42", preview: "Your invoice is due.", at: now - 3 * hour, seen: false, flagged: true, importance: HubMailEnvelopeImportance.high),
            Self.envelope(4, "mail-1", "INBOX", from: "Newsletter", subject: "This week", preview: "Five things to read.", at: now - 4 * hour),
        ], "Drafts": [], "Sent": [], "Archive": Self.archive(now: now), "Junk": [Self.envelope(6, "mail-1", "Junk", from: "Nobody", subject: "Broken message", preview: "", at: now)], "Trash": []],
        "mail-2": ["INBOX": [Self.envelope(5, "mail-2", "INBOX", from: "Ops Team", subject: "Deploy window", preview: "Friday at noon.", at: now - hour)], "Drafts": [], "Sent": [], "Archive": [], "Trash": []]]
    }

    var accounts: [HubMailAccount] {
        ["mail-1": "me@example.test", "mail-2": "work@example.test"].sorted { $0.key < $1.key }.map { id, email in
            HubMailAccount(id: id, name: names[id] ?? email, email: email, imap: "imap.example.test:993", smtp: "smtp.example.test:587", auth: "password",
                           signatures: signatures[id] ?? [], defaultSignature: defaults[id])
        }
    }

    private static let roles = ["INBOX": "inbox", "Drafts": "drafts", "Sent": "sent", "Archive": "archive", "Junk": "junk", "Trash": "trash"]
    private static let order = ["INBOX", "Drafts", "Sent", "Archive", "Junk", "Trash"]

    private func folders(_ account: String) -> [HubMailFolder] {
        Self.order.filter { mail[account]?[$0] != nil }.map { name in
            HubMailFolder(name: name, role: Self.roles[name] ?? "other", unread: Int64(mail[account]?[name]?.filter { !$0.seen }.count ?? 0),
                          label: name == "INBOX" ? "Inbox" : name)
        }
    }

    private func message(_ envelope: HubMailEnvelope) -> HubMailMessage {
        let people = [HubMailAddress(name: "Me", address: "me@example.test"), HubMailAddress(name: "", address: "team@example.test")]
        if let sent = bodies[envelope.id] {
            return HubMailMessage(envelope: envelope, to: sent.to.map { HubMailAddress(name: "", address: $0) }, cc: [], bcc: [], text: sent.text,
                                  html: sent.html ?? "", files: [], messageId: "<\(envelope.id)@example.test>", references: sent.references ?? [])
        }
        if envelope.id == 3 {
            return HubMailMessage(envelope: envelope, to: people, cc: [], bcc: [], text: envelope.preview, html: "<h2>Invoice 42</h2><p>Your invoice is due.</p>", files: [], messageId: "<3@example.test>", references: [])
        }
        guard envelope.id == 1 else {
            return HubMailMessage(envelope: envelope, to: people, cc: [], bcc: [], text: envelope.preview, html: "", files: [], messageId: "<\(envelope.id)@example.test>", references: [])
        }
        let files = [HubMailFile(part: "2", name: "logo.png", mime: "image/png", size: 68, contentId: "logo@x", inline: true),
                     HubMailFile(part: "3", name: "q3.pdf", mime: "application/pdf", size: 3, contentId: "", inline: false)]
        return HubMailMessage(envelope: envelope, to: people, cc: [HubMailAddress(name: "Sam Lee", address: "sam@example.test")], bcc: [],
                              text: "The figures are attached.", html: "<h2>Q3 figures</h2><p>The figures are attached.</p><img src=\"cid:logo@x\" width=\"40\"><a href=\"https://example.test\">Report</a>",
                              files: files, messageId: "<1@example.test>", references: [])
    }

    /// Answers a `mail…` route, or nil when it's one the fixture leaves to the rest of the server.
    func answer(_ request: URLRequest, method: String, path: String, body: () throws -> Data) throws -> Data? {
        let encoder = JSONEncoder()
        let parts = path.split(separator: "/").map(String.init)
        let query = URLComponents(url: request.url ?? URL(filePath: "/"), resolvingAgainstBaseURL: false)?.queryItems ?? []
        let folder = query.first { $0.name == "folder" }?.value ?? "INBOX"
        switch (method, parts.count, parts.dropFirst().first, parts.last) {
        case ("POST", 2, "uploads", _):
            let upload = try JSONDecoder().decode(HubMailUpload.self, from: body())
            nextID += 1
            uploads["up-\(nextID)"] = upload
            return try encoder.encode(HubMailAttach(id: "up-\(nextID)"))
        case ("POST", 2, "send", _):
            return try encoder.encode(send(try JSONDecoder().decode(HubMailSend.self, from: body())))
        case ("PATCH", 2, let account?, _):
            names[account] = try JSONDecoder().decode(HubMailSetup.self, from: body()).name
            return try encoder.encode(accounts.first { $0.id == account })
        case ("GET", 3, let account?, "folders"):
            return try encoder.encode(folders(account))
        case ("GET", 3, let account?, "health"):
            return try encoder.encode(accounts.first { $0.id == account })
        case ("PUT", 3, let account?, "signatures"):
            let input = try JSONDecoder().decode(HubMailSignatures.self, from: body())
            signatures[account] = input.signatures
            defaults[account] = input.defaultSignature
            return Data()
        case ("GET", 3, let account?, "messages"):
            let search = (query.first { $0.name == "query" }?.value ?? "").lowercased().replacingOccurrences(of: "subject ", with: "").replacingOccurrences(of: "\"", with: "")
            let list = mail[account]?[folder] ?? []
            let page = max(Int(query.first { $0.name == "page" }?.value ?? "") ?? 1, 1)
            let found = search.isEmpty ? list : list.filter { ($0.subject + " " + $0.from).lowercased().contains(search) }
            return try encoder.encode(Array(found.dropFirst((page - 1) * 50).prefix(50)))
        case ("PATCH", 3, let account?, "messages"):
            change(account, folder, try JSONDecoder().decode(HubMailChange.self, from: body()))
            return Data()
        case ("GET", 4, _, "6"):
            return Data("{}".utf8)
        case ("GET", 4, let account?, let id?):
            guard let found = mail[account]?[folder]?.first(where: { String($0.id) == id }) else { throw URLError(.fileDoesNotExist) }
            return try encoder.encode(message(found))
        case ("GET", 6, _, _):
            return Self.png
        default:
            return nil
        }
    }

    private func send(_ input: HubMailSend) -> HubMailSent {
        nextID += 1
        let id = nextID
        if let old = input.replaces { mail[input.account]?[old.folder]?.removeAll { $0.id == old.id } }
        let folder = input.draft == true ? "Drafts" : "Sent"
        var row = Self.envelope(id, input.account, folder, from: input.to.first ?? "Draft", subject: input.subject, preview: String(input.text.prefix(60)), at: Int64(Date.now.timeIntervalSince1970 * 1000))
        row.draft = input.draft == true
        row.attachments = Int64(input.files?.count ?? 0)
        mail[input.account]?[folder, default: []].insert(row, at: 0)
        bodies[id] = input
        return HubMailSent(draft: input.draft == true ? HubMailDraft(id: id, folder: folder) : nil)
    }

    private func change(_ account: String, _ folder: String, _ input: HubMailChange) {
        var list = mail[account]?[folder] ?? []
        if input.erase == true {
            list.removeAll { input.ids.contains($0.id) }
        } else if let target = input.folder {
            let moved = list.filter { input.ids.contains($0.id) }.map { row in
                var value = row
                value.folder = target
                return value
            }
            list.removeAll { input.ids.contains($0.id) }
            let existing = mail[account]?[target] ?? []
            mail[account]?[target] = moved + existing
        }
        for index in list.indices where input.ids.contains(list[index].id) {
            if let seen = input.seen { list[index].seen = seen }
            if let flagged = input.flagged { list[index].flagged = flagged }
        }
        mail[account]?[folder] = list
    }
}
#endif
