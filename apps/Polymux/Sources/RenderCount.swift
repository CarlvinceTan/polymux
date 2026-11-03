#if DEBUG
import SwiftUI

/// Counts view updates, so a flow can check that a gesture stays local to what it touches.
@MainActor
enum RenderCount {
    static let enabled = CommandLine.arguments.contains("--verify-renders")
    private static var counts: [String: Int] = [:]

    static func hit(_ name: String) {
        if enabled { counts[name, default: 0] += 1 }
    }

    static var summary: String {
        counts.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }.joined(separator: ",")
    }
}

/// Writes the counts to the data folder on a timer of its own, so reading them adds none.
struct RenderProbe: View {
    var body: some View {
        if RenderCount.enabled, let folder = ProcessInfo.processInfo.environment["POLYMUX_DATA_DIR"] {
            Color.clear.frame(width: 0, height: 0).task {
                while !Task.isCancelled {
                    try? RenderCount.summary.write(toFile: folder + "/renders.txt", atomically: true, encoding: .utf8)
                    try? await Task.sleep(for: .milliseconds(100))
                }
            }
        }
    }
}
#endif
