import AppKit
@preconcurrency import ApplicationServices
import Foundation

func emit(_ response: [String: Any]) {
    guard JSONSerialization.isValidJSONObject(response),
          let data = try? JSONSerialization.data(withJSONObject: response, options: [.sortedKeys]) else { exit(1) }
    (data + Data([10])).withUnsafeBytes { bytes in
        var offset = 0
        while offset < bytes.count {
            let count = Darwin.write(STDOUT_FILENO, bytes.baseAddress!.advanced(by: offset), bytes.count - offset)
            if count < 0 && errno == EINTR { continue }
            guard count > 0 else { _exit(1) }
            offset += count
        }
    }
}

@main
struct Entry {
    @MainActor
    static func main() async {
        signal(SIGPIPE, SIG_IGN)
        do {
            guard (2...3).contains(CommandLine.arguments.count) else {
                throw Refusal("usage", "Usage: control permissions|desktop|request-accessibility|windows|inspect|capture|session [JSON request]")
            }
            let operation = CommandLine.arguments[1]
            let request = try Request(data: Data((CommandLine.arguments.count == 3 ? CommandLine.arguments[2] : "{}").utf8))
            if operation == "session" {
                try session(request)
                return
            }
            let watchdog = Watchdog()
            watchdog.arm(seconds: 10)
            defer { watchdog.cancel() }
            if operation == "desktop" {
                emit(["ok": true, "summary": "Desktop attention sample; no input dispatched.",
                      "frontmost": NSWorkspace.shared.frontmostApplication?.processIdentifier ?? 0])
                return
            }
            if operation == "permissions" {
                emit(["ok": true, "summary": "Control supports bounded native actions in an owned-instance stdio session.",
                      "accessibility": AXIsProcessTrusted(), "screenRecording": CGPreflightScreenCaptureAccess(),
                      "executable": Bundle.main.executableURL?.path ?? CommandLine.arguments[0],
                      "operations": ["permissions", "desktop", "windows", "inspect", "capture", "session"],
                      "setupOperations": ["request-accessibility"],
                      "mutation": "session-only", "permissionPrompts": "never requested"])
                return
            }
            if operation == "request-accessibility" {
                let options = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
                let trusted = AXIsProcessTrustedWithOptions(options)
                emit(["ok": true, "accessibility": trusted,
                      "executable": Bundle.main.executableURL?.path ?? CommandLine.arguments[0],
                      "summary": trusted ? "Accessibility access is already granted." : "Accessibility approval requested. The user must approve the responsible host/helper in System Settings; approval has not been verified."])
                return
            }
            guard ["windows", "inspect", "capture"].contains(operation) else {
                throw Refusal("unsupported_operation", "Use a native session for actions. No one-shot input or foreground fallback is available.")
            }
            let target = try Target(request.instancePath(), fixture: request.fixture ?? false)
            var result: [String: Any]
            switch operation {
            case "windows":
                result = ["windows": try windows(target).map(\.json), "complete": true]
            case "inspect":
                let id = try request.windowID()
                _ = try exactWindow(target, id: id)
                let observer = try Session(target)
                let matches = try observer.windowRows().filter { $0["captureWindow"] as? UInt32 == id }
                guard matches.count == 1, let window = matches[0]["window"] as? String else {
                    throw Refusal("window_identity_unavailable", "The requested capture window has no unique native AX window.")
                }
                result = try observer.read(SessionRequest(JSONSerialization.data(withJSONObject: [
                    "op": "snapshot", "window": window, "maxElements": request.maxElements ?? 500,
                    "maxDepth": request.maxDepth ?? 30, "budgetMS": request.budgetMS ?? 2000])))
                result["elements"] = (result["elements"] as? [[String: Any]])?.map { row in
                    row.filter { $0.key != "token" }
                }
                result["actionTokens"] = "Use a live session to retain action handles."
                _ = try exactWindow(target, id: id)
                result["window"] = id
            default:
                let id = try request.windowID()
                _ = try exactWindow(target, id: id)
                guard let output = request.output else {
                    throw Refusal("output_required", "Pass a new absolute PNG output path; captures may contain private UI.")
                }
                result = try await capture(target, id: id, output: output)
                result["window"] = id
            }
            try target.validate()
            result["ok"] = true
            result["target"] = target.json
            result["coordinates"] = "WindowServer desktop points, top-left origin; PNG dimensions are pixels."
            result["summary"] = "\(operation) completed for Polymux \(target.path.number), process generation \(target.process.generation). No input was dispatched."
            emit(result)
        } catch let error as Refusal {
            emit(["ok": false, "code": error.code, "summary": error.summary])
            exit(1)
        } catch is DecodingError {
            emit(["ok": false, "code": "invalid_request", "summary": "Invalid request field type; no input was dispatched."])
            exit(1)
        } catch {
            // OS errors can contain private paths or values; do not echo them into routine diagnostics.
            emit(["ok": false, "code": "observation_failed", "summary": "Native observation failed; this is not an empty result. No input was dispatched."])
            exit(1)
        }
    }
}

final class Watchdog: @unchecked Sendable {
    private let lock = NSLock()
    private var generation = 0

    func cancel() { lock.lock(); generation += 1; lock.unlock() }

    func arm(seconds: Double) {
        lock.lock(); generation += 1; let current = generation; lock.unlock()
        DispatchQueue.global().asyncAfter(deadline: .now() + seconds) { [self] in
            lock.lock()
            guard generation == current else { lock.unlock(); return }
            // A reader that stopped draining stdout must not prevent deadline cleanup.
            DispatchQueue.global().asyncAfter(deadline: .now() + 0.1) { _exit(1) }
            emit(["ok": false, "code": "operation_timeout", "summary": "Control timed out. An in-flight action may have taken effect; observe before retrying."])
            _exit(1)
        }
    }
}

@MainActor
func session(_ request: Request) throws {
    let watchdog = Watchdog()
    watchdog.arm(seconds: 10)
    defer { watchdog.cancel() }
    let session = try Session(Target(request.instancePath(), fixture: request.fixture ?? false))
    emit(["ok": true, "summary": "Native session ready; no input dispatched.", "session": session.id, "target": session.target.json])
    while true {
        watchdog.arm(seconds: 120)
        guard let line = readLine() else { return }
        watchdog.arm(seconds: 10)
        do {
            let request = try SessionRequest(Data(line.utf8))
            if request.op == "close" {
                emit(["ok": true, "summary": "Session closed."])
                return
            }
            var result = try session.handle(request)
            result["ok"] = true
            result["summary"] = result["effect"] as? String == "unknown"
                ? "\(request.op) outcome is unknown; observe before proceeding and never replay automatically."
                : "\(request.op) completed for the owned native session."
            result["target"] = session.target.json
            emit(result)
        } catch let error as Refusal {
            emit(["ok": false, "code": error.code, "summary": error.summary])
        } catch {
            emit(["ok": false, "code": "invalid_request", "summary": "The session request failed; inspect state before retrying an action."])
        }
    }
}
