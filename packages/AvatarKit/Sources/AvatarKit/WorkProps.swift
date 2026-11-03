import CoreGraphics
import Foundation

public enum WorkProp: String, CaseIterable, Sendable {
    case keyboard, writing, chart, reading, mail, planning, card, plane

    public static func activity(_ mood: MoodID?) -> WorkProp? {
        switch mood {
        case .working: .keyboard
        case .writing: .writing
        case .reading, .searching: .reading
        case .sending: .mail
        default: nil
        }
    }

    public var label: String { rawValue.capitalized }

    public func layers(elapsed: Double, duration: Double = 5, reducedMotion: Bool = false) -> [CharacterLayer] {
        let time = reducedMotion ? duration : max(0, elapsed)
        let progress = clamp(time / max(0.01, duration))
        let art = PROP_ART[self] ?? []
        var bounds: [String: CGRect] = [:]
        for shape in art {
            let key = shape.group.isEmpty ? shape.part : shape.group
            bounds[key] = (bounds[key] ?? .null).union(shape.path.boundingBoxOfPath)
        }
        return art.compactMap { shape in
            let name = shape.group.isEmpty ? shape.part : shape.group
            let box = bounds[name] ?? .zero
            var transform = CGAffineTransform.identity
            var alpha = shape.alpha
            var path = shape.path
            func scale(_ x: Double, _ y: Double, bottom: Bool = false) -> CGAffineTransform {
                let origin = CGPoint(x: box.midX, y: bottom ? box.maxY : box.midY)
                return CGAffineTransform(translationX: origin.x, y: origin.y)
                    .scaledBy(x: x, y: y).translatedBy(x: -origin.x, y: -origin.y)
            }
            if name.contains("w-key"), !reducedMotion {
                let phase = (time + (name.contains("key-b") ? 0.25 : 0)).truncatingRemainder(dividingBy: 0.5) / 0.5
                let press = phase < 0.25 ? ease(phase / 0.25) : 1 - ease((phase - 0.25) / 0.35)
                transform = CGAffineTransform(translationX: 0, y: 1.6 * press)
            } else if name == "w-mag", !reducedMotion {
                let phase = time.truncatingRemainder(dividingBy: 1.6) / 0.4
                let points = [CGPoint.zero, CGPoint(x: -9, y: 4), CGPoint(x: -16, y: 10), CGPoint(x: -6, y: 8), .zero]
                let index = min(3, Int(phase)), k = ease(phase - Double(index))
                transform = CGAffineTransform(translationX: lerp(points[index].x, points[index + 1].x, k),
                                              y: lerp(points[index].y, points[index + 1].y, k))
            } else if name.contains("w-bar-") {
                let index = Int(name.suffix(1)) ?? 0
                let k = reducedMotion ? 1 : spring((time - duration * (0.06 + Double(index) * 0.18)) / 0.45)
                transform = scale(1, lerp(0.08, 1, k), bottom: true)
            } else if name.contains("w-check-") {
                let index = Int(name.suffix(1)) ?? 0
                let k = reducedMotion ? 1 : spring((time - duration * (0.15 + Double(index) * 0.27)) / 0.5)
                transform = scale(k, k)
            } else if name.contains("w-line-") {
                let index = Int(name.suffix(1)) ?? 0
                let k = clamp((progress - Double(index) * 0.2 - 0.02) / 0.15)
                let clip = CGRect(x: box.minX - 1, y: box.minY - 1, width: (box.width + 2) * k, height: box.height + 2)
                path = path.intersection(CGPath(rect: clip, transform: nil))
            } else if name == "w-pencil" {
                let from = [CGPoint(x: 20.2, y: 38.1), CGPoint(x: 27.9, y: 46.7), CGPoint(x: 35, y: 54.6),
                            CGPoint(x: 42.1, y: 62.5), CGPoint(x: 49.1, y: 70.4)]
                let to = [CGPoint(x: 47.3, y: 25.7), CGPoint(x: 62.5, y: 30.8), CGPoint(x: 66.5, y: 40.1),
                          CGPoint(x: 76.6, y: 46.6), CGPoint(x: 71.7, y: 60)]
                let index = min(4, Int(progress * 5))
                let k = ease((progress - Double(index) * 0.2 - 0.02) / 0.15)
                var tip = CGPoint(x: lerp(from[index].x, to[index].x, k) - 20.2,
                                  y: lerp(from[index].y, to[index].y, k) - 38.1)
                if progress > 0.97 {
                    let finish = ease((progress - 0.97) / 0.03)
                    tip = CGPoint(x: lerp(tip.x, 0, finish), y: lerp(tip.y, -6, finish))
                }
                transform = CGAffineTransform(translationX: tip.x, y: tip.y)
            } else if name == "w-plane" {
                let k = ease((progress - 0.74) / 0.1), settle = ease((progress - 0.84) / 0.16)
                alpha *= k
                transform = scale(lerp(lerp(0.3, 1.06, k), 1, settle), lerp(lerp(0.3, 1.06, k), 1, settle))
                    .translatedBy(x: lerp(lerp(-8, 1, k), 0, settle), y: lerp(lerp(10, -2, k), 0, settle))
            }
            guard alpha > 0.001, !path.isEmpty else { return nil }
            return CharacterLayer(path: path.copy(using: &transform)!, color: shape.color == "currentColor" ? "#7398D6" : shape.color,
                                  front: true, surface: false, alpha: alpha)
        }
    }
}

private func ease(_ t: Double) -> Double { mascotEase(t) }
private func spring(_ t: Double) -> Double {
    mascotEase(t, 0.34, 1.3, 0.64, 1)
}

struct PropShape: @unchecked Sendable {
    let path: CGPath
    let color: String
    let alpha: Double
    let group, part: String

    init(_ data: String, color: String, alpha: Double, group: String, part: String) {
        self.path = propPath(data); self.color = color == "#fff" ? "#FFFFFF" : color; self.alpha = alpha
        self.group = group; self.part = part
    }
}

// The reference generator emits only absolute moves, relative lines and closes.
private func propPath(_ data: String) -> CGPath {
    let regex = try! NSRegularExpression(pattern: "[Mlz]|[-+]?(?:[0-9]*\\.)?[0-9]+")
    let source = data as NSString
    let tokens = regex.matches(in: data, range: NSRange(location: 0, length: source.length)).map { source.substring(with: $0.range) }
    let path = CGMutablePath()
    var cursor = 0, command = "M", point = CGPoint.zero
    while cursor < tokens.count {
        if ["M", "l", "z"].contains(tokens[cursor]) { command = tokens[cursor]; cursor += 1 }
        if command == "z" { path.closeSubpath(); continue }
        guard cursor + 1 < tokens.count, let x = Double(tokens[cursor]), let y = Double(tokens[cursor + 1]) else { break }
        cursor += 2
        if command == "M" { point = CGPoint(x: x, y: y); path.move(to: point) }
        else { point.x += x; point.y += y; path.addLine(to: point) }
    }
    return path
}
