import Foundation

public enum HubMailMode: Sendable {
    case new, reply, replyAll, forward
}

/// A file placed in a message being written. Its offset is where it sits in the text.
public struct HubMailPiece: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var contentID: String
    public var offset: Int

    public init(id: String, name: String, contentID: String, offset: Int) {
        self.id = id
        self.name = name
        self.contentID = contentID
        self.offset = offset
    }
}

/// One run of text between the files in a message being written, then the file after it.
public struct HubMailSegment: Hashable, Sendable {
    public var from: Int
    public var text: String
    public var file: HubMailPiece?
}

/// What leaves the app for a message, and how a reply reads. Kept pure so it can be tested.
public enum HubMailFormat {
    private static func escape(_ value: String) -> String {
        value.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    private static func withoutTrailingSpace(_ value: String) -> String {
        var out = value
        while out.last?.isWhitespace == true { out.removeLast() }
        return out
    }

    /// The composer keeps text and signature apart, so changing the menu never rewrites what was typed.
    public static func body(_ text: String, signature: String) -> String {
        let ending = signature.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !ending.isEmpty else { return text }
        let message = withoutTrailingSpace(text)
        return message.isEmpty ? ending : "\(message)\n\n\(ending)"
    }

    /// The HTML alternative, needed only for a formatted signature or placed files.
    public static func html(_ text: String, signature: String, files: [HubMailPiece] = []) -> String? {
        let ending = signature.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !ending.isEmpty || !files.isEmpty else { return nil }
        let authored = marked(withoutTrailingSpace(text), files: files)
        guard !ending.isEmpty else { return "<div>\(authored)</div>" }
        let closing = "<div data-polymux-signature=\"true\">\(ending)</div>"
        return authored.isEmpty ? closing : "<div>\(authored)</div><br><br>\(closing)"
    }

    private static func marked(_ text: String, files: [HubMailPiece]) -> String {
        var out = ""
        var from = text.startIndex
        for file in ordered(files) {
            let offset = text.index(text.startIndex, offsetBy: min(max(file.offset, 0), text.count))
            let end = max(offset, from)
            out += escape(String(text[from..<end])).replacingOccurrences(of: "\n", with: "<br>")
            out += "<div><a href=\"cid:\(escape(file.contentID))\">\(escape(file.name))</a></div>"
            from = end
        }
        return out + escape(String(text[from...])).replacingOccurrences(of: "\n", with: "<br>")
    }

    private static func ordered(_ files: [HubMailPiece]) -> [HubMailPiece] {
        files.enumerated().sorted { ($0.element.offset, $0.offset) < ($1.element.offset, $1.offset) }.map(\.element)
    }

    /// Cuts the text at each file, so the composer can draw them where they were put.
    public static func segments(_ text: String, files: [HubMailPiece]) -> [HubMailSegment] {
        var out: [HubMailSegment] = []
        var from = 0
        for file in ordered(files) {
            let offset = max(from, min(file.offset, text.count))
            out.append(HubMailSegment(from: from, text: slice(text, from, offset), file: file))
            from = offset
        }
        out.append(HubMailSegment(from: from, text: slice(text, from, text.count), file: nil))
        return out
    }

    private static func slice(_ text: String, _ from: Int, _ to: Int) -> String {
        String(text[text.index(text.startIndex, offsetBy: from)..<text.index(text.startIndex, offsetBy: to)])
    }

    /// Replaces one segment's text. Only the files after it move, so a card already drawn can't be dragged elsewhere.
    public static func edit(_ draft: inout HubMailCompose, segment index: Int, text value: String) {
        let segments = segments(draft.text, files: draft.files)
        guard segments.indices.contains(index) else { return }
        let segment = segments[index]
        let delta = value.count - segment.text.count
        draft.text = slice(draft.text, 0, segment.from) + value + slice(draft.text, segment.from + segment.text.count, draft.text.count)
        guard delta != 0 else { return }
        let following = Set(segments[index...].compactMap(\.file?.id))
        for position in draft.files.indices where following.contains(draft.files[position].id) {
            draft.files[position].offset += delta
        }
    }

    public static func prefixed(_ subject: String, mode: HubMailMode) -> String {
        let prefix = mode == .forward ? "Fwd:" : "Re:"
        return subject.lowercased().hasPrefix(prefix.lowercased()) ? subject : "\(prefix) \(subject)"
    }

    public static func quote(_ message: HubMailMessage) -> String {
        let date = Date(timeIntervalSince1970: Double(message.envelope.at) / 1000).formatted(date: .abbreviated, time: .shortened)
        let head = "On \(date), \(message.envelope.from) wrote:"
        return ([head] + message.text.components(separatedBy: "\n").map { "> \($0)" }).joined(separator: "\n")
    }

    /// Who an answer goes to: never the mailbox itself, and nobody twice.
    public static func recipients(_ message: HubMailMessage, all: Bool, mine: String) -> [String] {
        var seen = Set<String>()
        var addresses = [message.envelope.address]
        if all { addresses += (message.to + message.cc).map(\.address) }
        return addresses.filter { address in
            let key = address.trimmingCharacters(in: .whitespaces).lowercased()
            return !key.isEmpty && key != mine.lowercased() && seen.insert(key).inserted
        }
    }

    public static func addresses(_ value: String) -> [String] {
        value.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    public static func baseSubject(_ subject: String) -> String {
        var out = subject.trimmingCharacters(in: .whitespaces)
        while let range = out.range(of: #"^(re|fwd|fw)\s*:\s*"#, options: [.regularExpression, .caseInsensitive]) {
            out.removeSubrange(range)
        }
        return out.trimmingCharacters(in: .whitespaces).lowercased()
    }

    public static func isAnswer(_ subject: String) -> Bool {
        subject.range(of: #"^\s*(re|fwd|fw)\s*:"#, options: [.regularExpression, .caseInsensitive]) != nil
    }

    /// How many messages each conversation on show holds. A run of identical subjects
    /// with no reply among them, such as an alert, isn't a conversation.
    public static func chains(_ list: [HubMailEnvelope]) -> [String: Int] {
        var groups: [String: (count: Int, replied: Bool)] = [:]
        for item in list {
            let base = baseSubject(item.subject)
            guard !base.isEmpty else { continue }
            let found = groups[base] ?? (0, false)
            groups[base] = (found.count + 1, found.replied || isAnswer(item.subject) || item.answered)
        }
        return groups.mapValues { $0.replied ? $0.count : 1 }
    }

    public static func chain(_ found: [HubMailEnvelope], subject: String) -> [HubMailEnvelope] {
        let base = baseSubject(subject)
        let chain = found.filter { baseSubject($0.subject) == base }
        return chain.count > 1 && chain.contains(where: { isAnswer($0.subject) || $0.answered }) ? chain : []
    }

    /// A short label for a file: its subtype, as a card shows it.
    public static func fileType(_ file: HubMailFile) -> String {
        let subtype = file.mime.split(separator: "/").last.map(String.init) ?? ""
        return String((subtype.isEmpty ? "file" : subtype).prefix(5)).uppercased()
    }
}

/// A message as the page its reader shows, with its files where the sender put them.
public enum HubMailPage {
    /// Fetches one of the message's parts for the reader.
    public static let partScheme = "hub-part"
    /// Asks the app to open a part.
    public static let openScheme = "hub-open"

    private static func escape(_ value: String) -> String {
        value.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    private static func cid(_ value: String) -> String {
        var out = value.trimmingCharacters(in: .whitespaces)
        if out.lowercased().hasPrefix("cid:") { out.removeFirst(4) }
        return (out.removingPercentEncoding ?? out).trimmingCharacters(in: CharacterSet(charactersIn: "<>")).lowercased()
    }

    private static func card(_ file: HubMailFile) -> String {
        let source = "\(partScheme)://file/\(file.part)"
        var preview = ""
        #if os(macOS)
        // iOS has no embedded PDF view, and a card there opens the file in Quick Look.
        if file.mime == "application/pdf" { preview = "<embed class=\"pdf\" src=\"\(source)\" type=\"application/pdf\">" }
        #endif
        if file.mime.hasPrefix("image/") { preview = "<img src=\"\(source)\" alt=\"\">" }
        else if file.mime.hasPrefix("video/") { preview = "<video controls preload=\"metadata\" src=\"\(source)\"></video>" }
        else if file.mime.hasPrefix("audio/") { preview = "<audio controls preload=\"metadata\" src=\"\(source)\"></audio>" }
        return "<section class=\"hub-file\"><a class=\"hub-head\" href=\"\(openScheme)://file/\(file.part)\">"
            + "<span>\(escape(HubMailFormat.fileType(file)))</span><strong>\(escape(file.name))</strong></a>\(preview)</section>"
    }

    private static func replace(_ html: String, pattern: String, with build: (NSTextCheckingResult, NSString) -> String) -> String {
        guard let regex = try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive, .dotMatchesLineSeparators]) else { return html }
        let source = html as NSString
        var out = ""
        var from = 0
        for match in regex.matches(in: html, range: NSRange(location: 0, length: source.length)) {
            out += source.substring(with: NSRange(location: from, length: match.range.location - from))
            out += build(match, source)
            from = match.range.location + match.range.length
        }
        return out + source.substring(from: from)
    }

    /// Resolves `cid:` links and images where they were authored. Images that could only break
    /// are dropped, since `http:` is refused and a `cid:` with no part has nothing to show.
    /// Files the body never places follow it.
    public static func document(_ message: HubMailMessage, dark: Bool) -> String {
        let files = Dictionary(message.files.filter { !$0.contentId.isEmpty }.map { (cid($0.contentId), $0) }, uniquingKeysWith: { first, _ in first })
        var used = Set<String>()
        var content: String
        if message.html.isEmpty {
            content = "<div class=\"hub-plain\">\(escape(message.text))</div>"
        } else {
            content = replace(message.html, pattern: #"<a\b[^>]*\bhref\s*=\s*["']cid:([^"']+)["'][^>]*>.*?</a>"#) { match, source in
                guard let file = files[cid(source.substring(with: match.range(at: 1)))] else { return "" }
                used.insert(file.part)
                return card(file)
            }
            content = replace(content, pattern: #"<img\b[^>]*\bsrc\s*=\s*["']\s*(cid:|http:)([^"']*)["'][^>]*>"#) { match, source in
                let tag = source.substring(with: match.range)
                guard source.substring(with: match.range(at: 1)).lowercased() == "cid:",
                      let file = files[cid(source.substring(with: match.range(at: 2)))] else { return "" }
                used.insert(file.part)
                guard file.mime.hasPrefix("image/") else { return card(file) }
                let address = "cid:" + source.substring(with: match.range(at: 2))
                return tag.replacingOccurrences(of: address, with: "\(partScheme)://file/\(file.part)", options: .caseInsensitive)
            }
        }
        let rest = message.files.filter { !used.contains($0.part) }.map(card).joined()
        return "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">"
            + "<style>\(style(plain: message.html.isEmpty, dark: dark))</style></head><body>\(content)\(rest)</body></html>"
    }

    /// Designed mail assumes a white page, so only plain text follows the app's appearance.
    private static func style(plain: Bool, dark: Bool) -> String {
        let text = plain && dark ? "#f2f2f7" : "#1c1c1e"
        let page = plain ? "transparent" : "#fff"
        let card = dark && plain ? "rgba(255,255,255,.08)" : "rgba(0,0,0,.05)"
        return """
        html{color-scheme:\(plain && dark ? "dark" : "light")}
        body{margin:0;padding:\(plain ? "0" : "12px");background:\(page);color:\(text);font:15px -apple-system,system-ui,sans-serif;line-height:1.45;overflow-wrap:anywhere}
        body>:first-child{margin-top:0}
        img{max-width:100%;height:auto}
        .hub-plain{white-space:pre-wrap}
        .hub-file{margin:12px 0;padding:8px;border-radius:8px;background:\(card)}
        .hub-head{display:flex;gap:8px;align-items:center;color:inherit;text-decoration:none}
        .hub-head span{font-size:10px;font-weight:600;opacity:.6}
        .hub-head strong{font-weight:500;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
        .hub-file img,.hub-file video,.hub-file audio,.hub-file embed{display:block;width:100%;margin-top:8px;border-radius:6px}
        .hub-file embed.pdf{height:420px}
        """
    }

    /// A signature as a small page, for its preview in the composer.
    public static func signature(_ html: String, text: String, dark: Bool) -> String {
        let content = html.isEmpty ? "<div class=\"hub-plain\">\(escape(text))</div>" : html
        let color = dark ? "#f2f2f7" : "#1c1c1e"
        return "<!doctype html><html><head><meta charset=\"utf-8\"><style>html{color-scheme:\(dark ? "dark" : "light")}"
            + "body{margin:0;background:transparent;color:\(color);font:12px Helvetica,Arial,sans-serif;line-height:1.55}.hub-plain{white-space:pre-wrap}</style></head><body>\(content)</body></html>"
    }
}
