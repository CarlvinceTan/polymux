import Foundation

struct SSEParser {
    private var line: [UInt8] = []
    private var payload: [String] = []

    mutating func feed(_ byte: UInt8) throws -> Event? {
        if byte == 10 { return try completeLine() }
        line.append(byte)
        return nil
    }

    mutating func finish() throws -> Event? {
        if !line.isEmpty { _ = try completeLine() }
        return try completeEvent()
    }

    private mutating func completeLine() throws -> Event? {
        if line.last == 13 { line.removeLast() }
        let text = String(decoding: line, as: UTF8.self)
        line.removeAll(keepingCapacity: true)
        if text.isEmpty { return try completeEvent() }
        if text.hasPrefix("data:") {
            var value = text.dropFirst(5)
            if value.first == " " { value = value.dropFirst() }
            payload.append(String(value))
        }
        return nil
    }

    private mutating func completeEvent() throws -> Event? {
        guard !payload.isEmpty else { return nil }
        let data = Data(payload.joined(separator: "\n").utf8)
        payload.removeAll(keepingCapacity: true)
        guard !data.isEmpty else { return nil }
        return try JSONDecoder().decode(Event.self, from: data)
    }
}
