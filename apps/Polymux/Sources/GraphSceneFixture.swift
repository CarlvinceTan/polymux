#if DEBUG
import CoreGraphics
import Foundation
import ImageIO
import PolymuxKit

/// Larger scenes for looking at the Graph Visualiser: a film being made, and a crowded day.
enum GraphSceneFixture {
    static let video = CommandLine.arguments.contains("--verify-graph-video")
    static let crowd = CommandLine.arguments.contains("--verify-graph-crowd")
    static let extras = ["Juno", "Kai", "Lumi", "Otto", "Pia", "Rex"]

    private static let stamp = "2026-11-01T00:00:00Z"

    private static func run(_ agent: String, trigger: String = RunTrigger.peer, status: String = RunStatus.running, summary: String,
                            parent: String? = nil, external: ExternalAgent? = nil, tools: [ToolActivity] = []) -> Run {
        Run(jobID: "run-" + agent, id: "run-" + agent, conversationID: "t-" + agent, trigger: trigger, parentRunID: parent, tools: tools, attempt: 1, seq: 1,
            createdAt: stamp,
            updatedAt: stamp, activity: "", agentID: "t-" + agent, status: status, summary: summary, external: external)
    }

    private static func tool(_ id: String, _ name: String, _ summary: String, status: String = ToolActivityStatus.completed) -> ToolActivity {
        ToolActivity(id: id, runID: "run", name: name, status: status, summary: summary)
    }

    private static func delivery(_ id: String, _ from: String, _ to: String, _ intent: String, _ status: String = PeerDeliveryStatus.delivered) -> PeerDelivery {
        PeerDelivery(id: id, sourceAgentID: "t-" + from, targetAgentID: "t-" + to, conversationID: "t-" + from, parentRunID: "run-" + from, intent: intent,
                     status: status, createdAt: stamp)
    }

    // MARK: A film

    /// Chief directs a launch film: the script and storyboard are done, three bots are on characters, shots and voice,
    /// and the rest hasn't started.
    static var videoWork: WorkActivity {
        WorkActivity(build: "abc1234", runs: [
            run("chief", trigger: RunTrigger.message, summary: "Directing the launch film", tools: [tool("v-brief", "read", "film/brief.md")]),
            run("aria", summary: "Finding reference footage", tools: [tool("v-web", "http_request", "pexels.com/search/city-night", status: ToolActivityStatus.running)]),
            run("milo", trigger: RunTrigger.child, summary: "Drawing Mia’s turnaround", parent: "run-milo",
                external: ExternalAgent(agent: "claude-acp", name: "Claude Code"), tools: [tool("v-image", "generate_image", "Mia, turnaround")]),
            run("nova", summary: "Rendering the shots", tools: [tool("v-ffmpeg", "bash", "ffmpeg -i shot-02.mov shot-02.mp4")]),
            run("fern", summary: "Recording the voiceover"),
        ], deliveries: [
            delivery("v1", "chief", "milo", PeerDeliveryIntent.request),
            delivery("v2", "chief", "fern", PeerDeliveryIntent.request),
            delivery("v3", "milo", "nova", PeerDeliveryIntent.handoff),
            delivery("v4", "nova", "milo", PeerDeliveryIntent.question, PeerDeliveryStatus.queued),
            delivery("v5", "aria", "chief", PeerDeliveryIntent.result, PeerDeliveryStatus.completed),
            // A crossing between two islands, so a boat is in the water on the map.
            delivery("v6", "aria", "milo", PeerDeliveryIntent.request, PeerDeliveryStatus.delivered),
        ])
    }

    static var videoGraph: WorkGraph {
        func step(_ id: String, _ title: String, _ symbol: String, _ state: String, _ subtitle: String) -> WorkNode {
            WorkNode(id: "step:" + id, title: title, parent: "film", kind: WorkKind.step, subtitle: subtitle, symbol: symbol, state: state)
        }
        func made(_ id: String, _ title: String, in step: String, _ kind: String, _ note: String, pictured: Bool = true) -> WorkNode {
            WorkNode(id: id, title: title, parent: "step:" + step, kind: kind, subtitle: note, preview: pictured ? "film/\(id).png" : nil,
                     details: [WorkDetail(label: "Path", value: "film/\(title)")])
        }
        let nodes = [
            WorkNode(id: "film", title: "Launch film", kind: WorkKind.pipeline, symbol: "film.stack"),
            step("script", "Script", "text.alignleft", WorkNodeState.done, "Written by Aria"),
            step("board", "Storyboard", "rectangle.split.3x1", WorkNodeState.done, "12 frames"),
            step("characters", "Characters", "person.2", WorkNodeState.working, "2 of 3 sheets"),
            step("shots", "Shots", "video", WorkNodeState.working, "2 of 6 rendered"),
            step("voice", "Voiceover", "waveform", WorkNodeState.working, "Line 4 of 9"),
            step("music", "Music", "music.note", WorkNodeState.idle, "Not started"),
            step("edit", "Edit", "scissors", WorkNodeState.idle, "Not started"),
            step("review", "Review", "checkmark.seal", WorkNodeState.idle, "Not started"),
            made("script-md", "script.md", in: "script", WorkKind.document, "3 pages", pictured: false),
            made("board-1", "Frame 1, the city", in: "board", WorkKind.image, "Wide"),
            made("board-2", "Frame 2, Mia wakes", in: "board", WorkKind.image, "Close"),
            made("board-3", "Frame 3, the robot", in: "board", WorkKind.image, "Two shot"),
            made("char-mia", "Mia", in: "characters", WorkKind.image, "Turnaround"),
            made("char-robo", "Robo", in: "characters", WorkKind.image, "Turnaround"),
            made("shot-01", "shot-01.mp4", in: "shots", WorkKind.video, "4 s"),
            made("shot-02", "shot-02.mp4", in: "shots", WorkKind.video, "6 s"),
            made("voice-take", "voiceover.m4a", in: "voice", WorkKind.audio, "0:41", pictured: false),
        ]
        var wires = zip(["script", "board", "characters", "shots", "voice", "music", "edit"], ["board", "characters", "shots", "voice", "music", "edit", "review"])
            .map { WorkWire(id: "next:" + $0, from: "step:" + $0, to: "step:" + $1) }
        wires += [
            WorkWire(id: "milo-step", from: "agent:t-milo", to: "step:characters", state: WorkWireState.active, label: "Drawing Robo’s turnaround", carries: WorkKind.image),
            WorkWire(id: "nova-step", from: "agent:t-nova", to: "step:shots", state: WorkWireState.active, label: "Rendering shot 3 of 6", carries: WorkKind.video),
            WorkWire(id: "fern-step", from: "agent:t-fern", to: "step:voice", state: WorkWireState.active, label: "Recording line 4 of 9", carries: WorkKind.audio),
        ]
        return WorkGraph(revision: 1, nodes: nodes, wires: wires)
    }

    // MARK: A crowd

    static var crowdRuns: [Run] {
        [run("juno", summary: "Answering support mail"), run("kai", summary: "Planning the sprint"), run("lumi", summary: "Posting the update"),
         run("otto", status: RunStatus.failed, summary: "Build failed"), run("pia", summary: "Checking the calendar")]
    }

    static var crowdNodes: [WorkNode] {
        func app(_ id: String, _ title: String, _ symbol: String, _ tint: String, of agent: String) -> WorkNode {
            WorkNode(id: id, title: title, parent: "agent:t-" + agent, kind: WorkKind.app, symbol: symbol, tint: tint, state: WorkNodeState.working)
        }
        return [
            app("mail", "Mail", "envelope", WorkNodeTint.blue, of: "juno"), app("notes", "Notes", "note.text", WorkNodeTint.orange, of: "kai"),
            app("messages", "Messages", "message", WorkNodeTint.green, of: "lumi"), app("xcode", "Xcode", "hammer", WorkNodeTint.blue, of: "otto"),
            app("calendar", "Calendar", "calendar", WorkNodeTint.red, of: "pia"), app("music", "Music", "music.note", WorkNodeTint.red, of: "pia"),
            WorkNode(id: "repo:site", title: "site", parent: WorkGraphBuilder.serverDevice, kind: WorkKind.repo, subtitle: "main at 4be21aa"),
            WorkNode(id: "device:ipad", title: "Studio iPad", kind: WorkKind.tablet),
            WorkNode(id: "device:imac", title: "Office iMac", kind: WorkKind.desktop),
        ]
    }

    static var crowdWires: [WorkWire] {
        func use(_ agent: String, _ app: String, _ label: String, _ carries: String) -> WorkWire {
            WorkWire(id: "\(agent)-\(app)", from: "agent:t-" + agent, to: app, direction: WorkWireDirection.none, state: WorkWireState.active, label: label, carries: carries)
        }
        return [use("juno", "mail", "Replying to 4 threads", WorkKind.message), use("kai", "notes", "Writing the sprint plan", WorkKind.document),
                use("lumi", "messages", "Posting to the team", WorkKind.message), use("pia", "calendar", "Finding a free hour", WorkKind.table)]
    }

    // MARK: Pictures

    /// A small drawing for anything that claims a preview, different for each ID and the same every time.
    static func picture(_ id: String) -> Data {
        let width = 480
        let height = 320
        guard let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0, space: CGColorSpaceCreateDeviceRGB(),
                                      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return Data() }
        var seed = id.utf8.reduce(UInt64(7)) { ($0 &* 31) &+ UInt64($1) }
        func roll() -> CGFloat {
            seed = seed &* 6364136223846793005 &+ 1442695040888963407
            return CGFloat((seed >> 33) % 1000) / 1000
        }
        func color(_ hue: CGFloat, _ saturation: CGFloat, _ brightness: CGFloat) -> CGColor {
            let k = { (n: CGFloat) -> CGFloat in
                let x = (n + hue * 6).truncatingRemainder(dividingBy: 6)
                return brightness - brightness * saturation * max(0, min(x, 4 - x, 1))
            }
            return CGColor(red: k(5), green: k(3), blue: k(1), alpha: 1)
        }
        let w = CGFloat(width)
        let h = CGFloat(height)
        let hue = roll()
        func figure(at x: CGFloat, ground: CGFloat, size: CGFloat, hue: CGFloat) {
            context.setFillColor(color(hue, 0.55, 0.9))
            context.fillEllipse(in: CGRect(x: x - size * 0.3, y: ground, width: size * 0.6, height: size * 0.7))
            context.fillEllipse(in: CGRect(x: x - size * 0.24, y: ground + size * 0.62, width: size * 0.48, height: size * 0.48))
            context.setFillColor(CGColor(gray: 0.15, alpha: 1))
            context.fillEllipse(in: CGRect(x: x - size * 0.12, y: ground + size * 0.84, width: size * 0.07, height: size * 0.07))
            context.fillEllipse(in: CGRect(x: x + size * 0.05, y: ground + size * 0.84, width: size * 0.07, height: size * 0.07))
        }
        if id.hasPrefix("board") {
            // A pencil frame on paper.
            context.setFillColor(CGColor(red: 0.97, green: 0.95, blue: 0.9, alpha: 1))
            context.fill(CGRect(x: 0, y: 0, width: w, height: h))
            context.setStrokeColor(CGColor(gray: 0.25, alpha: 1))
            context.setLineWidth(5)
            context.stroke(CGRect(x: 24, y: 24, width: w - 48, height: h - 48))
            context.setLineWidth(3)
            let horizon = h * (0.35 + roll() * 0.2)
            context.strokeLineSegments(between: [CGPoint(x: 24, y: horizon), CGPoint(x: w - 24, y: horizon)])
            for index in 0..<5 {
                let x = 50 + CGFloat(index) * 84 + roll() * 20
                context.stroke(CGRect(x: x, y: horizon, width: 50, height: 60 + roll() * 110))
            }
            figure(at: w * (0.3 + roll() * 0.4), ground: 40, size: 110, hue: hue)
        } else if id.hasPrefix("char") {
            // A character sheet: the same figure three times.
            context.setFillColor(color(hue, 0.18, 0.98))
            context.fill(CGRect(x: 0, y: 0, width: w, height: h))
            for (index, size) in [150.0, 190.0, 150.0].enumerated() {
                figure(at: w * (0.2 + CGFloat(index) * 0.3), ground: 50, size: size, hue: hue)
            }
        } else {
            // A shot: sky, sun and hills.
            let sky = [color(hue, 0.5, 0.95), color((hue + 0.08).truncatingRemainder(dividingBy: 1), 0.25, 1)] as CFArray
            if let gradient = CGGradient(colorsSpace: CGColorSpaceCreateDeviceRGB(), colors: sky, locations: [0, 1]) {
                context.drawLinearGradient(gradient, start: CGPoint(x: 0, y: h), end: .zero, options: [])
            }
            context.setFillColor(CGColor(red: 1, green: 0.9, blue: 0.5, alpha: 1))
            context.fillEllipse(in: CGRect(x: w * (0.15 + roll() * 0.6), y: h * 0.55, width: 70, height: 70))
            for layer in 0..<3 {
                context.setFillColor(color((hue + 0.45).truncatingRemainder(dividingBy: 1), 0.45, 0.75 - CGFloat(layer) * 0.18))
                context.move(to: CGPoint(x: 0, y: 0))
                var x: CGFloat = 0
                while x <= w {
                    context.addLine(to: CGPoint(x: x, y: h * (0.42 - CGFloat(layer) * 0.12) + sin(x / 60 + roll() * 6) * 26))
                    x += 40
                }
                context.addLine(to: CGPoint(x: w, y: 0))
                context.fillPath()
            }
            figure(at: w * 0.5, ground: 20, size: 90, hue: (hue + 0.3).truncatingRemainder(dividingBy: 1))
        }
        guard let image = context.makeImage() else { return Data() }
        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, "public.png" as CFString, 1, nil) else { return Data() }
        CGImageDestinationAddImage(destination, image, nil)
        CGImageDestinationFinalize(destination)
        return data as Data
    }
}
#endif
