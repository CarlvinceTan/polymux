import ApplicationServices
import Darwin

// Exact window routing uses macOS SPI; unavailable symbols refuse input.
@MainActor
enum Pointer {
    typealias Window = @convention(c) (AXUIElement, UnsafeMutablePointer<CGWindowID>) -> Int32
    typealias Location = @convention(c) (CGEvent, Double, Double) -> Void
    typealias Post = @convention(c) (pid_t, CGEvent) -> Void
    typealias Field = @convention(c) (CGEvent, UInt32, Int64) -> Void
    static let ax = dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", RTLD_LAZY)
    static let sky = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY)

    static func key(_ code: CGKeyCode, target: Target) throws {
        guard let sky, let symbol = dlsym(sky, "SLEventPostToPid"),
              let source = CGEventSource(stateID: .hidSystemState),
              let down = CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: true),
              let up = CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: false) else {
            throw Refusal("route_unavailable", "Process-targeted keyboard routing is unavailable.")
        }
        let post = unsafeBitCast(symbol, to: Post.self)
        down.flags = []; up.flags = []
        try target.validate()
        post(target.process.pid, down)
        Thread.sleep(forTimeInterval: 0.012)
        post(target.process.pid, up)
    }

    static func window(_ element: AXUIElement) throws -> CGWindowID {
        guard let ax, let symbol = dlsym(ax, "_AXUIElementGetWindow") else {
            throw Refusal("route_unavailable", "Native window identity is unavailable on this macOS version.")
        }
        var id: CGWindowID = 0
        guard unsafeBitCast(symbol, to: Window.self)(element, &id) == 0, id != 0 else {
            throw Refusal("window_identity_unavailable", "The retained native window has no exact WindowServer identity.")
        }
        return id
    }

    static func frame(_ element: AXUIElement) throws -> CGRect {
        guard let position = try attribute(element, kAXPositionAttribute), CFGetTypeID(position) == AXValueGetTypeID(),
              let size = try attribute(element, kAXSizeAttribute), CFGetTypeID(size) == AXValueGetTypeID() else {
            throw Refusal("geometry_unavailable", "The native target has no readable frame.")
        }
        var origin = CGPoint.zero, sizeValue = CGSize.zero
        guard AXValueGetValue(position as! AXValue, .cgPoint, &origin), AXValueGetValue(size as! AXValue, .cgSize, &sizeValue),
              [origin.x, origin.y, sizeValue.width, sizeValue.height].allSatisfy(\.isFinite),
              sizeValue.width > 0, sizeValue.height > 0 else {
            throw Refusal("geometry_unavailable", "The native target has no finite visible frame.")
        }
        return CGRect(origin: origin, size: sizeValue)
    }

    static func send(_ request: SessionRequest, handle: Handle, target: Target) throws {
        guard let sky,
              let locationSymbol = dlsym(sky, "CGEventSetWindowLocation"),
              let postSymbol = dlsym(sky, "SLEventPostToPid"),
              let fieldSymbol = dlsym(sky, "SLEventSetIntegerValueField") else {
            throw Refusal("route_unavailable", "Exact native window event routing is unavailable on this macOS version.")
        }
        let location = unsafeBitCast(locationSymbol, to: Location.self)
        let post = unsafeBitCast(postSymbol, to: Post.self)
        let field = unsafeBitCast(fieldSymbol, to: Field.self)
        let id = try self.window(handle.root)
        let window = try exactWindow(target, id: id)
        var bounds = try frame(handle.element).intersection(window.bounds)
        var current: AXUIElement? = handle.element
        for _ in 0..<100 {
            guard let element = current else { break }
            if CFEqual(element, handle.root) { break }
            if try attribute(element, kAXRoleAttribute) as? String == kAXScrollAreaRole {
                bounds = bounds.intersection(try frame(element))
            }
            current = try attribute(element, kAXParentAttribute) as! AXUIElement?
        }
        guard !bounds.isNull, !bounds.isEmpty, window.onscreen == true else {
            throw Refusal("offscreen_target", "Reveal the target in its native window before sending pointer input.")
        }
        let point = CGPoint(x: bounds.midX, y: bounds.midY)
        guard let source = CGEventSource(stateID: .hidSystemState) else {
            throw Refusal("event_unavailable", "Could not create a native event source.")
        }
        let group = Int64.random(in: 1...Int64(Int32.max))
        func stamp(_ event: CGEvent, button: Int64 = 0, click: Int64 = 0) {
            event.flags = []
            event.location = point
            location(event, point.x - window.bounds.minX, point.y - window.bounds.minY)
            for (key, value): (UInt32, Int64) in [(40, Int64(target.process.pid)),
                                                (51, Int64(id)), (91, Int64(id)), (92, Int64(id))] {
                field(event, key, value)
            }
            if event.type != .scrollWheel {
                for (key, value): (UInt32, Int64) in [(1, click), (3, button), (7, 3), (58, group)] { field(event, key, value) }
            }
        }
        guard let move = CGEvent(mouseEventSource: source, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left) else {
            throw Refusal("event_unavailable", "Could not create the native pointer event.")
        }
        var events: [CGEvent] = []
        if request.op == "menu" {
            for type in [CGEventType.rightMouseDown, .rightMouseUp] {
                guard let event = CGEvent(mouseEventSource: source, mouseType: type, mouseCursorPosition: point, mouseButton: .right) else {
                    throw Refusal("event_unavailable", "Could not create the native click pair.")
                }
                stamp(event, button: 1, click: 1)
                events.append(event)
            }
        } else {
            let direction = request.direction ?? ""
            guard ["up", "down", "left", "right"].contains(direction) else { throw Refusal("invalid_request", "Choose a scroll direction.") }
            let vertical: Int32 = direction == "up" ? 2 : direction == "down" ? -2 : 0
            let horizontal: Int32 = direction == "left" ? 2 : direction == "right" ? -2 : 0
            for _ in 0..<(request.amount ?? 3) {
                guard let event = CGEvent(scrollWheelEvent2Source: source, units: .line, wheelCount: 2,
                                          wheel1: vertical, wheel2: horizontal, wheel3: 0) else {
                    throw Refusal("event_unavailable", "Could not create the native scroll event.")
                }
                stamp(event)
                events.append(event)
            }
        }
        try target.validate()
        guard try self.window(handle.root) == id,
              try exactWindow(target, id: id).bounds == window.bounds else {
            throw Refusal("stale_window", "The native window changed while preparing input; read again.")
        }
        stamp(move)
        func dispatch(_ event: CGEvent) {
            if request.op == "scroll" { event.postToPid(target.process.pid) }
            else { post(target.process.pid, event) }
        }
        dispatch(move)
        Thread.sleep(forTimeInterval: 0.012)
        for event in events {
            dispatch(event)
            Thread.sleep(forTimeInterval: 0.03)
        }
    }
}
