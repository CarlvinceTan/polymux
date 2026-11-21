// Draws the installer window's background into Background.tiff (1x and 2x).
// Run after changing the art or the layout in scripts/lib/dmg-layout.py:
//   swift apps/Polymux/Installer/background.swift
// Finder always draws icon labels black over a picture, in light and dark mode,
// so the area under the icons stays light.
import AppKit

let width: CGFloat = 600, height: CGFloat = 360
// Icon centres from scripts/lib/dmg-layout.py, measured from the top.
let app = CGPoint(x: 160, y: 150), folder = CGPoint(x: 440, y: 150)
let iconSize: CGFloat = 128

let paper = NSColor(srgbRed: 0.973, green: 0.969, blue: 0.992, alpha: 1)  // PAPER in dmg-layout.py
let accent = NSColor(srgbRed: 0.353, green: 0.294, blue: 0.839, alpha: 1)
let ink = NSColor(srgbRed: 0.231, green: 0.216, blue: 0.318, alpha: 1)
let cue = "Drag Polymux to Applications to install"

func draw() {
    // Flip so coordinates match Finder's, which run from the top left.
    let context = NSGraphicsContext.current!.cgContext
    context.translateBy(x: 0, y: height)
    context.scaleBy(x: 1, y: -1)

    paper.setFill()
    CGRect(x: 0, y: 0, width: width, height: height).fill()

    // Arrow between the icons, level with their centres.
    let start = app.x + iconSize / 2 + 30, end = folder.x - iconSize / 2 - 30, head: CGFloat = 13
    let arrow = NSBezierPath()
    arrow.move(to: CGPoint(x: start, y: app.y))
    arrow.line(to: CGPoint(x: end, y: app.y))
    arrow.move(to: CGPoint(x: end - head, y: app.y - head))
    arrow.line(to: CGPoint(x: end, y: app.y))
    arrow.line(to: CGPoint(x: end - head, y: app.y + head))
    arrow.lineWidth = 5
    arrow.lineCapStyle = .round
    arrow.lineJoinStyle = .round
    accent.setStroke()
    arrow.stroke()

    // The cue sits below the Finder labels.
    let style = NSMutableParagraphStyle()
    style.alignment = .center
    let text = NSAttributedString(string: cue, attributes: [
        .font: NSFont.systemFont(ofSize: 14, weight: .medium),
        .foregroundColor: ink,
        .paragraphStyle: style,
    ])
    let size = text.size()
    NSGraphicsContext.saveGraphicsState()
    let flipped = NSGraphicsContext(cgContext: context, flipped: true)
    NSGraphicsContext.current = flipped
    text.draw(in: CGRect(x: 0, y: 290 - size.height / 2, width: width, height: size.height))
    NSGraphicsContext.restoreGraphicsState()
}

func render(scale: CGFloat) -> NSBitmapImageRep {
    let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: Int(width * scale), pixelsHigh: Int(height * scale),
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    rep.size = NSSize(width: width, height: height)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    draw()
    NSGraphicsContext.restoreGraphicsState()
    return rep.retagging(with: .sRGB)!
}

let folderURL = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
let output = folderURL.appendingPathComponent("Background.tiff")
let temporary = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
try FileManager.default.createDirectory(at: temporary, withIntermediateDirectories: true)
defer { try? FileManager.default.removeItem(at: temporary) }
var images: [String] = []
for (scale, name) in [(1.0, "Background.png"), (2.0, "Background@2x.png")] {
    let url = temporary.appendingPathComponent(name)
    try render(scale: scale).representation(using: .png, properties: [:])!.write(to: url)
    images.append(url.path)
}
// One compressed TIFF with both resolutions; Finder picks the one for the display.
func tiffutil(_ arguments: [String]) throws {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/tiffutil")
    process.arguments = arguments
    try process.run()
    process.waitUntilExit()
    guard process.terminationStatus == 0 else { exit(1) }
}
let combined = temporary.appendingPathComponent("Background.tiff").path
try tiffutil(["-cathidpicheck"] + images + ["-out", combined])
try tiffutil(["-lzw", combined, "-out", output.path])
print(output.path)
