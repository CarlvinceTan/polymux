import Foundation

struct Refusal: Error {
    let code: String
    let summary: String

    init(_ code: String, _ summary: String) {
        self.code = code
        self.summary = summary
    }
}

struct Request: Decodable {
    let instance: String?
    let fixture: Bool?
    let window: UInt32?
    let output: String?
    let maxElements: Int?
    let maxDepth: Int?
    let budgetMS: Int?

    enum CodingKeys: String, CodingKey, CaseIterable {
        case instance, fixture, window, output, maxElements, maxDepth, budgetMS
    }

    init(data: Data) throws {
        guard let object = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              Set(object.keys).isSubset(of: Set(CodingKeys.allCases.map(\.rawValue))) else {
            throw Refusal("invalid_request", "Expected an object with only documented request fields.")
        }
        self = try JSONDecoder().decode(Self.self, from: data)
        guard (1...5000).contains(maxElements ?? 500),
              (1...100).contains(maxDepth ?? 30),
              (100...5000).contains(budgetMS ?? 2000) else {
            throw Refusal("invalid_budget", "Use maxElements 1–5000, maxDepth 1–100, budgetMS 100–5000.")
        }
    }

    func instancePath() throws -> String {
        guard let instance else {
            throw Refusal("instance_required", "Pass the exact data parent printed by scripts/run.sh.")
        }
        return instance
    }

    func windowID() throws -> UInt32 {
        guard let window, window > 0 else {
            throw Refusal("window_required", "Pass one exact window ID from windows; titles and list order are not identity.")
        }
        return window
    }
}

struct InstancePath {
    let url: URL
    let number: Int

    init(_ path: String, base: URL) throws {
        let url = URL(fileURLWithPath: path).standardizedFileURL
        guard path.hasPrefix("/"),
              url == url.resolvingSymlinksInPath(),
              url.deletingLastPathComponent() == base.standardizedFileURL.resolvingSymlinksInPath(),
              let number = Int(url.lastPathComponent), number > 0,
              String(number) == url.lastPathComponent else {
            throw Refusal("foreign_instance", "Only a live numbered scripts/run.sh instance is supported; symlink aliases are refused.")
        }
        self.url = url
        self.number = number
    }
}

struct ReadBudget {
    let maxElements: Int
    let maxDepth: Int
    let deadline: ContinuousClock.Instant
    private(set) var reasons: Set<String> = []
    private(set) var count = 0

    init(elements: Int, depth: Int, milliseconds: Int) {
        maxElements = elements
        maxDepth = depth
        deadline = .now.advanced(by: .milliseconds(milliseconds))
    }

    mutating func admit(depth: Int) -> Bool {
        if ContinuousClock.now >= deadline {
            reasons.insert("deadline")
            return false
        }
        if count >= maxElements {
            reasons.insert("maxElements")
            return false
        }
        if depth > maxDepth {
            reasons.insert("maxDepth")
            return false
        }
        count += 1
        return true
    }

    mutating func incomplete(_ reason: String) {
        reasons.insert(reason)
    }
}
