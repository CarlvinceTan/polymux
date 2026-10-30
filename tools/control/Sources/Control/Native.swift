import AppKit
import ApplicationServices
import Darwin
import ScreenCaptureKit

struct ProcessStamp: Equatable {
    let pid: pid_t
    let seconds: UInt64
    let microseconds: UInt64

    init(_ pid: pid_t) throws {
        var info = proc_bsdinfo()
        let size = Int32(MemoryLayout<proc_bsdinfo>.size)
        guard pid > 0, proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size,
              info.pbi_uid == getuid() else {
            throw Refusal("process_unavailable", "The owned process is missing or belongs to another user.")
        }
        self.pid = pid
        seconds = info.pbi_start_tvsec
        microseconds = info.pbi_start_tvusec
    }

    var generation: String { "\(pid):\(seconds):\(microseconds)" }
}

struct Target {
    let path: InstancePath
    let owner: ProcessStamp
    let process: ProcessStamp
    let bundle: URL
    let identifier: String
    let fixture: Bool

    init(_ instance: String, fixture: Bool = false) throws {
        self.fixture = fixture
        var buffer = [CChar](repeating: 0, count: Int(PATH_MAX))
        guard confstr(_CS_DARWIN_USER_TEMP_DIR, &buffer, buffer.count) > 0 else {
            throw Refusal("temp_unavailable", "Cannot resolve the runner's per-user temporary directory.")
        }
        let directory = String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
        let base = URL(fileURLWithPath: directory).appendingPathComponent("polymux-test")
        let path = try InstancePath(instance, base: base)
        self.path = path
        let attributes = try FileManager.default.attributesOfItem(atPath: path.url.path)
        guard attributes[.ownerAccountID] as? UInt32 == getuid() else {
            throw Refusal("foreign_instance", "The instance directory belongs to another user.")
        }
        func pid(_ name: String) throws -> pid_t {
            let file = path.url.appendingPathComponent(name + ".pid")
            guard file == file.resolvingSymlinksInPath(),
                  let data = try? Data(contentsOf: file), data.count < 32,
                  let text = String(data: data, encoding: .utf8),
                  let value = pid_t(text.trimmingCharacters(in: .whitespacesAndNewlines)), value > 0 else {
                throw Refusal("invalid_owner", "The runner's process identity files are missing or invalid.")
            }
            return value
        }
        owner = try ProcessStamp(pid("run"))
        process = try ProcessStamp(pid(fixture ? "fixture" : "app"))
        guard (process.seconds, process.microseconds) >= (owner.seconds, owner.microseconds) else {
            throw Refusal("stale_instance", "The app predates this runner; an older instance cannot be reused.")
        }
        bundle = path.url.appendingPathComponent(fixture ? "fixture/Polymux \(path.number) Fixture.app" : "Polymux \(path.number).app")
        identifier = "com.flarehq.polymux.app.test.\(path.number)" + (fixture ? ".fixture" : "")
        try validateApp()
    }

    private func validateApp() throws {
        guard let app = NSRunningApplication(processIdentifier: process.pid), !app.isTerminated,
              app.bundleIdentifier == identifier,
              app.bundleURL?.standardizedFileURL == bundle,
              app.executableURL?.standardizedFileURL == bundle.appendingPathComponent("Contents/MacOS/Polymux"),
              bundle == bundle.resolvingSymlinksInPath() else {
            throw Refusal("foreign_app", "The live process does not match this runner instance's bundle, executable and identifier.")
        }
    }

    func validate() throws {
        guard try ProcessStamp(owner.pid) == owner, try ProcessStamp(process.pid) == process else {
            throw Refusal("stale_process", "The runner or app process generation changed; rediscover the instance.")
        }
        let fresh = try Target(path.url.path, fixture: fixture)
        guard fresh.owner == owner, fresh.process == process else {
            throw Refusal("stale_instance", "The runner's current identity changed; rediscover the instance.")
        }
    }

    var json: [String: Any] {
        ["instance": path.url.path, "pid": process.pid, "generation": process.generation,
         "ownerGeneration": owner.generation, "bundle": bundle.path, "identifier": identifier]
    }
}

struct WindowRow {
    let id: CGWindowID
    let bounds: CGRect
    let onscreen: Bool?

    var json: [String: Any] {
        ["window": id, "onscreen": onscreen as Any? ?? NSNull(),
         "frame": ["x": bounds.minX, "y": bounds.minY, "width": bounds.width, "height": bounds.height]]
    }
}

func windows(_ target: Target) throws -> [WindowRow] {
    guard let rows = CGWindowListCopyWindowInfo(.optionAll, kCGNullWindowID) as? [[String: Any]] else {
        throw Refusal("windows_unavailable", "WindowServer enumeration failed; this is not an empty result.")
    }
    var result: [WindowRow] = []
    for row in rows {
        guard (row[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == target.process.pid else { continue }
        guard let layer = (row[kCGWindowLayer as String] as? NSNumber)?.intValue else {
            throw Refusal("window_metadata_unavailable", "An owned window's layer is unavailable; enumeration is incomplete.")
        }
        guard layer == 0 else { continue }
        guard let id = (row[kCGWindowNumber as String] as? NSNumber)?.uint32Value,
              let frame = row[kCGWindowBounds as String] as? NSDictionary,
              let bounds = CGRect(dictionaryRepresentation: frame) else {
            throw Refusal("window_metadata_unavailable", "An owned window's identity or geometry is unavailable; enumeration is incomplete.")
        }
        result.append(WindowRow(id: id, bounds: bounds, onscreen: row[kCGWindowIsOnscreen as String] as? Bool))
    }
    return result
}

func exactWindow(_ target: Target, id: CGWindowID) throws -> WindowRow {
    let matches = try windows(target).filter { $0.id == id }
    guard matches.count == 1 else {
        throw Refusal("window_missing", "The exact native window no longer belongs to this process generation.")
    }
    return matches[0]
}

func attribute(_ element: AXUIElement, _ name: String) throws -> CFTypeRef? {
    var value: CFTypeRef?
    let error = AXUIElementCopyAttributeValue(element, name as CFString, &value)
    switch error {
    case .success: return value
    case .noValue, .attributeUnsupported: return nil
    default: throw Refusal("ax_unavailable", "Accessibility attribute \(name) failed (AX error \(error.rawValue)); absence is unproven.")
    }
}

@MainActor
func capture(_ target: Target, id: CGWindowID, output: String) async throws -> [String: Any] {
    guard CGPreflightScreenCaptureAccess() else {
        throw Refusal("screen_recording_required", "Screen Recording approval is required. No permission prompt was requested.")
    }
    let destination = URL(fileURLWithPath: output)
    guard output.hasPrefix("/"), !FileManager.default.fileExists(atPath: output),
          destination.deletingLastPathComponent() == destination.deletingLastPathComponent().resolvingSymlinksInPath() else {
        throw Refusal("invalid_output", "Use a new absolute PNG path in an existing, non-symlinked directory. Captures may contain private UI.")
    }
    let content = try await SCShareableContent.excludingDesktopWindows(false, onScreenWindowsOnly: false)
    guard let window = content.windows.first(where: { $0.windowID == id && $0.owningApplication?.processID == target.process.pid }) else {
        throw Refusal("capture_window_missing", "ScreenCaptureKit cannot resolve the exact owned window.")
    }
    try target.validate()
    _ = try exactWindow(target, id: id)
    let filter = SCContentFilter(desktopIndependentWindow: window)
    let config = SCStreamConfiguration()
    config.width = max(1, Int(filter.contentRect.width * CGFloat(filter.pointPixelScale)))
    config.height = max(1, Int(filter.contentRect.height * CGFloat(filter.pointPixelScale)))
    config.showsCursor = false
    config.ignoreShadowsSingleWindow = true
    let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
    try target.validate()
    _ = try exactWindow(target, id: id)
    guard let data = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]) else {
        throw Refusal("capture_encoding_failed", "The exact-window image could not be encoded.")
    }
    // O_EXCL and mode 0600 prevent overwriting an existing file or publishing a world-readable capture.
    let fd = open(output, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, S_IRUSR | S_IWUSR)
    guard fd >= 0 else { throw Refusal("output_unavailable", "Cannot exclusively create the private capture file.") }
    var success = false
    defer {
        close(fd)
        if !success { unlink(output) }
    }
    try data.withUnsafeBytes { buffer in
        var offset = 0
        while offset < buffer.count {
            let count = Darwin.write(fd, buffer.baseAddress!.advanced(by: offset), buffer.count - offset)
            if count < 0, errno == EINTR { continue }
            guard count > 0 else { throw Refusal("output_failed", "Capture file write failed.") }
            offset += count
        }
    }
    success = true
    return ["output": output, "pixelWidth": image.width, "pixelHeight": image.height,
            "pointPixelScale": filter.pointPixelScale, "frame": ["x": window.frame.minX, "y": window.frame.minY,
            "width": window.frame.width, "height": window.frame.height]]
}
