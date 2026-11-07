import AppKit
import ApplicationServices
import Darwin

struct SessionRequest: Decodable {
    let op: String
    let window: String?
    let token: String?
    let text: String?
    let key: String?
    let direction: String?
    let amount: Int?
    let maxElements: Int?
    let maxDepth: Int?
    let budgetMS: Int?
    let compact: Bool?

    init(_ data: Data) throws {
        let allowed: Set<String> = ["op", "window", "token", "text", "key", "direction", "amount", "maxElements", "maxDepth", "budgetMS", "compact"]
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              Set(object.keys).isSubset(of: allowed), data.count <= 131072 else {
            throw Refusal("invalid_request", "Use documented session fields and at most 128 KiB per request.")
        }
        self = try JSONDecoder().decode(Self.self, from: data)
        guard (1...5000).contains(maxElements ?? 2000), (1...100).contains(maxDepth ?? 50),
              (100...5000).contains(budgetMS ?? 5000), (1...50).contains(amount ?? 3), (text?.utf16.count ?? 0) <= 16384 else {
            throw Refusal("invalid_request", "Invalid observation budget, scroll amount or text length.")
        }
    }
}

struct Handle {
    let element: AXUIElement
    let root: AXUIElement
    let secure: Bool
    let identity: [String: String]
    let deadline: ContinuousClock.Instant
}

@MainActor
final class Session {
    let target: Target
    let app: AXUIElement
    let id = UUID().uuidString
    private var surfaces: [(String, AXUIElement)] = []
    private var handles: [String: Handle] = [:]
    private var fence: Int32 = -1
    private var snapshot = 0

    init(_ target: Target) throws {
        guard AXIsProcessTrusted() else { throw Refusal("accessibility_required", "Accessibility access is required; no prompt was requested.") }
        self.target = target
        let deadline = ContinuousClock.now.advanced(by: .seconds(10))
        while try windows(target).isEmpty {
            guard ContinuousClock.now < deadline else { throw Refusal("window_missing", "The owned app has no window yet.") }
            Thread.sleep(forTimeInterval: 0.05)
            try target.validate()
        }
        app = AXUIElementCreateApplication(target.process.pid)
        AXUIElementSetMessagingTimeout(app, 0.3)
        let path = target.path.url.appendingPathComponent(target.fixture ? "control-fixture.lock" : "control-app.lock").path
        fence = Darwin.open(path, O_CREAT | O_RDWR | O_NOFOLLOW | O_CLOEXEC, 0o600)
        guard fence >= 0, flock(fence, LOCK_EX | LOCK_NB) == 0 else {
            if fence >= 0 { Darwin.close(fence); fence = -1 }
            throw Refusal("target_busy", "Another Control session owns this target; close it before opening another.")
        }
    }

    deinit { if fence >= 0 { Darwin.close(fence) } }

    func roots() throws -> [AXUIElement] {
        try target.validate()
        let app = AXUIElementCreateApplication(target.process.pid)
        AXUIElementSetMessagingTimeout(app, 0.3)
        guard let roots = try attribute(app, kAXWindowsAttribute) as? [AXUIElement], roots.count <= 128 else {
            throw Refusal("ax_windows_unavailable", "Native AX windows are unavailable or exceed the bound.")
        }
        surfaces.removeAll { item in !roots.contains { CFEqual($0, item.1) } }
        for root in roots where !surfaces.contains(where: { CFEqual($0.1, root) }) {
            surfaces.append(("w-" + UUID().uuidString, root))
        }
        return roots
    }

    func windowRows() throws -> [[String: Any]] {
        _ = try roots()
        let main = try attribute(app, kAXMainWindowAttribute)
        return try surfaces.map { token, element in
            var row = try describe(element)
            row["window"] = token
            if let id = try? Pointer.window(element), (try? exactWindow(target, id: id)) != nil { row["captureWindow"] = id }
            row["main"] = main.map { CFEqual($0, element) } ?? false
            return row
        }
    }

    func describe(_ element: AXUIElement) throws -> [String: Any] {
        AXUIElementSetMessagingTimeout(element, 0.3)
        var unavailable: [String] = []
        func read(_ name: String) -> CFTypeRef? {
            do { return try attribute(element, name) }
            catch { unavailable.append(name); return nil }
        }
        let role = read(kAXRoleAttribute) as? String ?? ""
        let subrole = read(kAXSubroleAttribute) as? String ?? ""
        guard !role.isEmpty, !unavailable.contains(kAXSubroleAttribute) else {
            throw Refusal("ax_identity_unavailable", "A native element's role or secure-field classification is unreadable.")
        }
        let secure = subrole == kAXSecureTextFieldSubrole || unavailable.contains(kAXSubroleAttribute)
        var row: [String: Any] = ["role": role, "subrole": subrole, "secure": secure]
        let title = read(kAXTitleAttribute) as? String ?? ""
        let description = read(kAXDescriptionAttribute) as? String ?? ""
        row["label"] = String((description.isEmpty ? title : description).prefix(1000))
        row["title"] = String(title.prefix(1000))
        if let identifier = read(kAXIdentifierAttribute) as? String { row["identifier"] = identifier }
        if !secure, let value = read(kAXValueAttribute) {
            if let value = value as? String { row["value"] = String(value.prefix(4000)) }
            else if let value = value as? NSNumber { row["value"] = value.stringValue }
        }
        if role == kAXStaticTextRole, (row["label"] as? String)?.isEmpty == true {
            row["label"] = row["value"] as? String ?? ""
        }
        if let enabled = read(kAXEnabledAttribute) as? Bool { row["enabled"] = enabled }
        if let focused = read(kAXFocusedAttribute) as? Bool { row["focused"] = focused }
        if let selected = read(kAXSelectedAttribute) as? Bool { row["selected"] = selected }
        if let position = read(kAXPositionAttribute), CFGetTypeID(position) == AXValueGetTypeID(),
           let size = read(kAXSizeAttribute), CFGetTypeID(size) == AXValueGetTypeID() {
            var origin = CGPoint.zero, dimensions = CGSize.zero
            if AXValueGetValue(position as! AXValue, .cgPoint, &origin), AXValueGetValue(size as! AXValue, .cgSize, &dimensions),
               [origin.x, origin.y, dimensions.width, dimensions.height].allSatisfy(\.isFinite) {
                row["frame"] = ["x": origin.x, "y": origin.y, "w": dimensions.width, "h": dimensions.height]
            }
        }
        var actions: CFArray?
        let error = AXUIElementCopyActionNames(element, &actions)
        if error == .success { row["actions"] = actions as? [String] ?? [] }
        else { row["actionsUnavailable"] = error.rawValue }
        row["unavailable"] = unavailable
        return row
    }

    func read(_ request: SessionRequest) throws -> [String: Any] {
        handles.removeAll()
        let windows = try roots()
        var selected = windows
        if let token = request.window {
            guard let root = surfaces.first(where: { $0.0 == token })?.1 else { throw Refusal("stale_window", "Discover current native windows again.") }
            selected = [root]
        }
        snapshot += 1
        var budget = ReadBudget(elements: request.maxElements ?? 2000, depth: request.maxDepth ?? 50, milliseconds: request.budgetMS ?? 5000)
        var rows: [[String: Any]] = []
        var seen: [AXUIElement] = []
        var queue = selected.reversed().map { ($0, $0, 0, Optional<Int>.none, 0) }
        var visited = 0
        while let (element, root, depth, parent, rawDepth) = queue.popLast() {
            guard !seen.contains(where: { CFEqual($0, element) }) else { continue }
            guard rawDepth <= 100 else { budget.incomplete("maxNativeDepth"); continue }
            guard budget.admit(depth: depth) else { continue }
            visited += 1
            seen.append(element)
            let index = rows.count
            do {
                AXUIElementSetMessagingTimeout(element, 0.3)
                if request.compact == true, [kAXGroupRole, kAXScrollAreaRole].contains(try attribute(element, kAXRoleAttribute) as? String ?? "") {
                    if let children = try attribute(element, kAXChildrenAttribute) as? [AXUIElement] {
                        let remaining = max(0, budget.maxElements - visited - queue.count)
                        if children.count > remaining { budget.incomplete("maxElements") }
                        queue.append(contentsOf: children.prefix(remaining).reversed().map { ($0, root, depth, parent, rawDepth + 1) })
                    }
                    continue
                }
                var row = try describe(element)
                let token = "\(id):\(snapshot):\(index)"
                row["index"] = index; row["depth"] = depth; row["parent"] = parent
                row["token"] = token
                rows.append(row)
                handles[token] = Handle(element: element, root: root, secure: row["secure"] as? Bool == true,
                                        identity: ["role": row["role"] as? String ?? "", "label": row["label"] as? String ?? "",
                                                   "identifier": row["identifier"] as? String ?? ""],
                                        deadline: .now.advanced(by: .seconds(10)))
                if row["secure"] as? Bool != true, let children = try attribute(element, kAXChildrenAttribute) as? [AXUIElement] {
                    let remaining = max(0, budget.maxElements - visited - queue.count)
                    if children.count > remaining { budget.incomplete("maxElements") }
                    queue.append(contentsOf: children.prefix(remaining).reversed().map { ($0, root, depth + 1, index, rawDepth + 1) })
                }
            } catch let error as Refusal { budget.incomplete(error.summary) }
            catch { budget.incomplete("axRead") }
        }
        if !budget.reasons.isEmpty { handles.removeAll() }
        try target.validate()
        return ["elements": rows, "complete": budget.reasons.isEmpty, "incompleteReasons": budget.reasons.sorted(),
                "windows": try windowRows(), "snapshot": "\(id):\(snapshot)", "values": "secure fields omitted"]
    }

    func resolve(_ token: String?) throws -> Handle {
        guard let token, let handle = handles[token], .now < handle.deadline else {
            throw Refusal("stale_token", "Read a complete fresh snapshot before acting; tokens expire after ten seconds or any action/read.")
        }
        guard try roots().contains(where: { CFEqual($0, handle.root) }) else {
            throw Refusal("stale_window", "The element's native window has closed.")
        }
        var pid: pid_t = 0
        guard AXUIElementGetPid(handle.element, &pid) == .success, pid == target.process.pid else {
            throw Refusal("stale_element", "The native element is no longer available in the owned process.")
        }
        let live = try describe(handle.element)
        guard handle.identity.allSatisfy({ (live[$0.key] as? String ?? "") == $0.value }) else {
            throw Refusal("stale_element", "The native element's role or identity changed; read again.")
        }
        var current: AXUIElement? = handle.element
        for _ in 0..<100 {
            guard let element = current else { break }
            if CFEqual(element, handle.root) { return handle }
            current = try attribute(element, kAXParentAttribute) as! AXUIElement?
        }
        throw Refusal("stale_element", "The element is no longer attached to its observed native window.")
    }

    func act(_ request: SessionRequest) throws -> [String: Any] {
        defer { handles.removeAll() }
        let handle = try resolve(request.token)
        guard try attribute(handle.element, kAXEnabledAttribute) as? Bool != false else {
            throw Refusal("disabled", "The target control is disabled.")
        }
        let front = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let pointer = CGEvent(source: nil)?.location
        let clipboard = NSPasteboard.general.changeCount
        handles.removeAll()
        let result: AXError
        var route = "accessibility"
        switch request.op {
        case "press", "menu":
            let action = request.op == "press" ? kAXPressAction : kAXShowMenuAction
            var actions: CFArray?
            guard AXUIElementCopyActionNames(handle.element, &actions) == .success else {
                throw Refusal("actions_unavailable", "Cannot read the native control's available actions.")
            }
            if (actions as? [String] ?? []).contains(action) {
                result = AXUIElementPerformAction(handle.element, action as CFString)
            } else if request.op == "menu" {
                try Pointer.send(request, handle: handle, target: target)
                result = .success; route = "window-event"
            } else {
                throw Refusal("unsupported_action", "The control does not advertise this native action.")
            }
        case "set", "type":
            guard let text = request.text else { throw Refusal("invalid_request", "Text is required.") }
            var settable: DarwinBoolean = false
            guard AXUIElementIsAttributeSettable(handle.element, kAXValueAttribute as CFString, &settable) == .success,
                  settable.boolValue else { throw Refusal("unsupported_action", "This control does not expose a writable native value.") }
            var value = text
            var insertion: Int?
            if request.op == "type" || handle.secure {
                guard AXUIElementSetAttributeValue(handle.element, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success else {
                    throw Refusal("focus_unavailable", "The text control did not accept native focus; no text was changed.")
                }
            }
            if request.op == "type" {
                guard !handle.secure else { throw Refusal("unsupported_action", "Use set for secure fields; their existing value is never read.") }
                let old = try attribute(handle.element, kAXValueAttribute) as? String ?? ""
                var range = CFRange(location: (old as NSString).length, length: 0)
                if let selected = try attribute(handle.element, kAXSelectedTextRangeAttribute), CFGetTypeID(selected) == AXValueGetTypeID() {
                    AXValueGetValue(selected as! AXValue, .cfRange, &range)
                }
                let source = old as NSString
                guard range.location >= 0, range.length >= 0, range.location <= source.length, range.length <= source.length - range.location else {
                    throw Refusal("invalid_selection", "Native text selection is outside the current value.")
                }
                value = source.replacingCharacters(in: NSRange(location: range.location, length: range.length), with: text)
                insertion = range.location + (text as NSString).length
            }
            let role = try attribute(handle.element, kAXRoleAttribute) as? String
            let native: CFTypeRef
            if role == kAXSliderRole {
                guard let number = Double(value), number.isFinite else { throw Refusal("invalid_request", "A slider needs a finite numeric value.") }
                native = NSNumber(value: number)
            } else { native = value as CFString }
            result = AXUIElementSetAttributeValue(handle.element, kAXValueAttribute as CFString, native)
            if result == .success, let insertion {
                var range = CFRange(location: insertion, length: 0)
                let error = AXUIElementSetAttributeValue(handle.element, kAXSelectedTextRangeAttribute as CFString,
                                                        AXValueCreate(.cfRange, &range)!)
                guard error == .success else {
                    throw Refusal("action_uncertain", "Text changed but its insertion point could not be set; observe before continuing.")
                }
            }
        case "key":
            guard let key = request.key else { throw Refusal("invalid_request", "A key is required.") }
            let keyResult = try sendKey(key, handle: handle)
            result = keyResult.0; route = keyResult.1
        case "scroll":
            try Pointer.send(request, handle: handle, target: target)
            result = .success; route = "window-event"
        default:
            throw Refusal("unsupported_operation", "No fallback input route is available for this operation.")
        }
        guard result == .success || result == .cannotComplete else {
            throw Refusal("action_uncertain", "Native action returned AX error \(result.rawValue). Observe before deciding what to do; do not replay automatically.")
        }
        try target.validate()
        guard front == NSWorkspace.shared.frontmostApplication?.processIdentifier,
              pointer == CGEvent(source: nil)?.location, clipboard == NSPasteboard.general.changeCount else {
            throw Refusal("interference_detected", "Desktop state changed during the action; its outcome needs observation.")
        }
        return ["dispatched": result == .success ? true : NSNull(),
                "effect": result == .success ? "observe" : "unknown", "axError": result.rawValue,
                "route": route, "tokensInvalidated": true]
    }

    func sendKey(_ key: String, handle: Handle) throws -> (AXError, String) {
        if key == "cmd+a" {
            guard !handle.secure, let text = try attribute(handle.element, kAXValueAttribute) as? String else {
                throw Refusal("unsupported_action", "Select-all requires a readable text field.")
            }
            var range = CFRange(location: 0, length: (text as NSString).length)
            return (AXUIElementSetAttributeValue(handle.element, kAXSelectedTextRangeAttribute as CFString,
                                                AXValueCreate(.cfRange, &range)!), "accessibility")
        }
        if key == "return" {
            var actions: CFArray?
            if AXUIElementCopyActionNames(handle.element, &actions) == .success,
               (actions as? [String] ?? []).contains(kAXConfirmAction) {
                return (AXUIElementPerformAction(handle.element, kAXConfirmAction as CFString), "accessibility")
            }
        }
        let keys: [String: CGKeyCode] = ["return": 36, "escape": 53, "delete": 51, "down": 125,
                                       "up": 126, "left": 123, "right": 124, "tab": 48, "space": 49]
        guard let code = keys[key] else { throw Refusal("unsupported_key", "This key is not supported.") }
        if !CFEqual(handle.element, handle.root) {
            guard AXUIElementSetAttributeValue(handle.element, kAXFocusedAttribute as CFString, kCFBooleanTrue) == .success,
                  try attribute(handle.element, kAXFocusedAttribute) as? Bool == true else {
                throw Refusal("focus_unavailable", "The native target did not accept keyboard focus; no key was posted.")
            }
        }
        guard let focused = try attribute(app, kAXFocusedWindowAttribute), CFEqual(focused, handle.root) else {
            throw Refusal("focus_unavailable", "The intended native window does not own this app's keyboard focus; no key was posted.")
        }
        try Pointer.key(code, target: target)
        return (.success, "process-key-event")
    }

    func handle(_ request: SessionRequest) throws -> [String: Any] {
        try target.validate()
        switch request.op {
        case "windows": return ["windows": try windowRows()]
        case "snapshot": return try read(request)
        case "press", "menu", "set", "type", "key", "scroll": return try act(request)
        default: throw Refusal("unsupported_operation", "Unsupported session operation; no input was dispatched.")
        }
    }
}
