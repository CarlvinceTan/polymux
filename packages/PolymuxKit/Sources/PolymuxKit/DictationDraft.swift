import Foundation

public struct DictationDraft {
    public private(set) var partial: NSRange?
    private var segment: Double?
    private var committed: Set<Double> = []

    public init() {}

    public mutating func stop() {
        partial = nil
        segment = nil
        committed.removeAll()
    }

    public mutating func update(_ transcript: String, start: Double, final: Bool,
                                text: String, selection: NSRange) -> (text: String, cursor: Int, range: NSRange)? {
        guard !committed.contains(start) else { return nil }
        guard !transcript.isEmpty || (segment == start && partial != nil) else { return nil }
        if segment != start { partial = nil }
        let range = partial ?? selection
        guard let bounds = Range(range, in: text) else { return nil }
        var updated = text
        updated.replaceSubrange(bounds, with: transcript)
        let inserted = NSRange(location: range.location, length: transcript.utf16.count)
        partial = final ? nil : inserted
        segment = start
        if final { committed.insert(start) }
        return (updated, NSMaxRange(inserted), range)
    }

    public mutating func edit(from old: String, to new: String) {
        guard let range = partial, old != new else { return }
        let before = Array(old.utf16)
        let after = Array(new.utf16)
        var prefix = 0
        while prefix < min(before.count, after.count), before[prefix] == after[prefix] { prefix += 1 }
        var suffix = 0
        while suffix < min(before.count, after.count) - prefix,
              before[before.count - suffix - 1] == after[after.count - suffix - 1] { suffix += 1 }
        let end = before.count - suffix
        if end <= range.location {
            partial = NSRange(location: range.location + after.count - before.count, length: range.length)
        } else if prefix < NSMaxRange(range) {
            // User edits own the text from this point; ignore revisions of this segment.
            if let segment { committed.insert(segment) }
            partial = nil
        }
    }
}
