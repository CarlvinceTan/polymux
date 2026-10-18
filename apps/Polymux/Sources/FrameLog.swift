import Foundation
import QuartzCore
#if os(macOS)
import AppKit
#endif

/// Timing hook for the Release scroll measurement.
/// With `--verify-frame-log <path>` the app records CADisplayLink frame intervals
/// (ms, one per line: wall time, interval) to <path>, flushed every 32 frames so a
/// force-quit (XCUIElement.terminate) loses at most a moment. Inert without the
/// argument; the display link keeps ticking while the main thread is up, so an
/// interval over 1.5x the display period means a frame was missed.
@MainActor
final class FrameLog {
    static func install() {
        let args = CommandLine.arguments
        guard let index = args.firstIndex(of: "--verify-frame-log"), args.indices.contains(index + 1) else { return }
        shared.start(path: args[index + 1], attempt: 0)
    }

    private static let shared = FrameLog()
    #if DEBUG
    private static let configuration = "Debug"
    #else
    private static let configuration = "Release"
    #endif
    private var link: CADisplayLink?
    private var handle: FileHandle?
    private var last: CFTimeInterval = 0
    private var pending = 0

    private func start(path: String, attempt: Int) {
        guard link == nil else { return }
        if handle == nil {
            guard FileManager.default.createFile(atPath: path, contents: Data("# wall time, frame interval ms [\(Self.configuration)]\n".utf8)),
                  let opened = try? FileHandle(forWritingTo: URL(fileURLWithPath: path)) else { return }
            handle = opened
        }
        #if os(macOS)
        guard let screen = NSScreen.main else {   // window server not up yet: retry for 10 s
            guard attempt < 40 else { return }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
                self?.start(path: path, attempt: attempt + 1)
            }
            return
        }
        let link = screen.displayLink(target: self, selector: #selector(tick))
        #else
        let link = CADisplayLink(target: self, selector: #selector(tick))
        #endif
        link.add(to: .main, forMode: .common)
        self.link = link
    }

    @objc private func tick(_ link: CADisplayLink) {
        defer { last = link.timestamp }
        guard last > 0, let handle else { return }
        let line = String(format: "%.3f %.3f\n", Date().timeIntervalSince1970, (link.timestamp - last) * 1000)
        handle.write(Data(line.utf8))
        pending += 1
        if pending == 32 {
            pending = 0
            try? handle.synchronize()
        }
    }
}
