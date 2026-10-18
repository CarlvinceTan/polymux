import SwiftUI
import AVFoundation
import Accelerate
import Combine

struct AudioView: View {
    let url: URL
    let name: String
    let bubbleColor: Color
    let ink: Color
    @State private var player: AVPlayer?
    @State private var cached: URL?
    @State private var samples: [Float] = []
    @State private var failure: String?
    @State private var playing = false
    @State private var time = 0.0
    @State private var duration = 0.0
    @State private var scrubbing = false
    @State private var resume = false
    private let timer = Timer.publish(every: 0.25, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 12) {
                Button(action: toggle) {
                    Image(systemName: playing ? "pause.fill" : "play.fill")
                        .font(.system(size: 18, weight: .semibold))
                        .foregroundStyle(ink)
                        .frame(width: 44, height: 44)
                        .background(ink.opacity(0.08), in: Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(playing ? "Pause" : "Play") \(name)")
                VStack(spacing: 5) {
                    waveform
                    HStack {
                        Text(clock(time))
                        Spacer(minLength: 4)
                        Text(duration > 0 ? clock(duration) : "–:––")
                    }
                    .font(.caption2).monospacedDigit().foregroundStyle(ink.opacity(0.65))
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("\(clock(time)) of \(duration > 0 ? clock(duration) : "unknown duration")")
                }
            }
            if let failure { Text(failure).font(.caption2).foregroundStyle(ink.opacity(0.65)) }
        }
        .padding(12)
        .frame(minWidth: 220, maxWidth: 320)
        .background(bubbleColor, in: RoundedRectangle(cornerRadius: 16))
        .task(id: url) { await prepare() }
        .onReceive(timer) { _ in
            guard let player else { return }
            let seconds = player.currentTime().seconds
            if seconds.isFinite, !scrubbing { time = seconds }
            if let value = player.currentItem?.duration.seconds, value.isFinite, value > 0 { duration = value }
            if !scrubbing { playing = player.timeControlStatus != .paused }
            if let error = player.currentItem?.error { failure = error.localizedDescription }
        }
        .onDisappear {
            player?.pause()
            playing = false
            scrubbing = false
            resume = false
        }
    }

    private var waveform: some View {
        GeometryReader { geometry in
            AudioWave(samples: samples, progress: duration > 0 ? time / duration : 0, ink: ink)
                .contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 0)
                    .onChanged { value in
                        guard duration > 0 else { return }
                        if !scrubbing {
                            scrubbing = true
                            resume = playing
                            player?.pause()
                        }
                        time = duration * min(1, max(0, value.location.x / max(geometry.size.width, 1)))
                    }
                    .onEnded { _ in
                        guard scrubbing else { return }
                        scrubbing = false
                        seek(time)
                        if resume { player?.play() }
                        resume = false
                    })
        }
        .frame(height: 32)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Audio position")
        .accessibilityValue("\(clock(time)) of \(duration > 0 ? clock(duration) : "unknown duration")")
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: seek(time + 5)
            case .decrement: seek(time - 5)
            @unknown default: break
            }
        }
    }

    private func prepare() async {
        do {
            let file = try await MediaCache.shared.file(url: url, name: name)
            try Task.checkCancellation()
            cached = file
            let wave = try await WaveCache.shared.load(file)
            try Task.checkCancellation()
            samples = wave.samples
            duration = wave.duration
        } catch is CancellationError {
        } catch { failure = "Couldn’t load the audio preview." }
    }

    private func toggle() {
        failure = nil
        if player == nil {
            player = AVPlayer(url: cached ?? url)
            if time > 0 { seek(time) }
        }
        if duration > 0, time >= duration - 0.1 { seek(0) }
        playing.toggle()
        if playing { player?.play() } else { player?.pause() }
    }

    private func seek(_ seconds: Double) {
        guard duration > 0 else { return }
        time = min(duration, max(0, seconds))
        player?.seek(to: CMTime(seconds: time, preferredTimescale: 600))
    }

    private func clock(_ seconds: Double) -> String {
        let value = seconds.isFinite ? max(0, Int(seconds)) : 0
        if value >= 3600 { return String(format: "%d:%02d:%02d", value / 3600, value / 60 % 60, value % 60) }
        return String(format: "%d:%02d", value / 60, value % 60)
    }
}

private struct AudioWave: View {
    let samples: [Float]
    let progress: Double
    let ink: Color

    var body: some View {
        Canvas { context, size in
            let bars = samples.isEmpty ? [Float](repeating: 0, count: 48) : samples
            let gap = 2.0
            let width = max(1, (size.width - gap * Double(bars.count - 1)) / Double(bars.count))
            for (index, sample) in bars.enumerated() {
                let height = max(3, size.height * Double(sample))
                let rect = CGRect(x: Double(index) * (width + gap), y: (size.height - height) / 2,
                                  width: width, height: height)
                let played = Double(index) / Double(bars.count) < progress
                context.fill(Path(roundedRect: rect, cornerRadius: width / 2),
                             with: .color(ink.opacity(played ? 0.9 : 0.28)))
            }
        }
    }
}

private nonisolated struct WaveData: Sendable {
    let samples: [Float]
    let duration: Double

    static func read(_ url: URL) throws -> WaveData {
        let file = try AVAudioFile(forReading: url, commonFormat: .pcmFormatFloat32, interleaved: false)
        let format = file.processingFormat
        guard file.length > 0, format.sampleRate.isFinite, format.sampleRate > 0,
              let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: 65_536) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        let count = 48
        var energy = [Double](repeating: 0, count: count)
        var lengths = [Int64](repeating: 0, count: count)
        var position: Int64 = 0
        while position < file.length {
            try Task.checkCancellation()
            try file.read(into: buffer)
            guard buffer.frameLength > 0, let channels = buffer.floatChannelData else { break }
            var offset = 0
            let frames = Int(buffer.frameLength)
            while offset < frames {
                let index = min(count - 1, Int(Double(position + Int64(offset)) / Double(file.length) * Double(count)))
                let end = Int64(ceil(Double(index + 1) * Double(file.length) / Double(count)))
                let segment = min(frames - offset, max(1, Int(end - position - Int64(offset))))
                for channel in 0..<Int(format.channelCount) {
                    var sum: Float = 0
                    vDSP_svesq(channels[channel].advanced(by: offset), 1, &sum, vDSP_Length(segment))
                    if sum.isFinite { energy[index] += Double(sum) }
                }
                lengths[index] += Int64(segment) * Int64(format.channelCount)
                offset += segment
            }
            position += Int64(frames)
        }
        let amplitudes = zip(energy, lengths).map { sqrt($0 / Double(max($1, 1))) }
        let peak = amplitudes.max() ?? 0
        let samples = amplitudes.map { peak > 0 ? Float(sqrt($0 / peak)) : 0 }
        return WaveData(samples: samples, duration: Double(file.length) / format.sampleRate)
    }
}

private actor WaveCache {
    static let shared = WaveCache()
    private var values: [URL: WaveData] = [:]
    private var order: [URL] = []

    func load(_ file: URL) async throws -> WaveData {
        if let value = values[file] { return value }
        let task = Task.detached(priority: .utility) { try WaveData.read(file) }
        let value = try await withTaskCancellationHandler {
            try await task.value
        } onCancel: { task.cancel() }
        if values[file] == nil { order.append(file) }
        values[file] = value
        if order.count > 64 { values.removeValue(forKey: order.removeFirst()) }
        return value
    }
}
