import AVFoundation
import PolymuxKit

enum MicEvent: Sendable {
    case frame(LivePCM, level: Float)
    case end
}

@MainActor
protocol LiveSound: AnyObject {
    var playing: Bool { get }
    var drained: (() -> Void)? { get set }
    func start(rate: Int) async throws -> AsyncThrowingStream<MicEvent, Error>
    func setMuted(_ muted: Bool)
    func setSilenced(_ silenced: Bool)
    func play(_ chunk: LivePCM) throws
    func interrupt()
    func stop()
}

extension LiveSound {
    func setSilenced(_ silenced: Bool) {}
}

@MainActor
final class NativeSound: LiveSound {
    var drained: (() -> Void)?
    var playing: Bool { buffers > 0 }
    private let audio = VoiceAudio()
    private var events: AsyncThrowingStream<MicEvent, Error>.Continuation?
    private var converter: AVAudioConverter?
    private var buffers = 0
    private var duration: Double = 0
    private var token = UUID()

    func start(rate: Int) async throws -> AsyncThrowingStream<MicEvent, Error> {
        stop()
        guard await AVAudioApplication.requestRecordPermission() else { throw VoiceFailure.microphone }
        try Task.checkCancellation()
        let stream = AsyncThrowingStream<MicEvent, Error>.makeStream(bufferingPolicy: .bufferingOldest(12))
        events = stream.continuation
        let capture = LiveCapture(rate: rate, events: stream.continuation)
        do { try audio.start(capture: capture) }
        catch { stop(); throw error }
        return stream.stream
    }

    func setSilenced(_ silenced: Bool) { audio.setSilenced(silenced) }

    func setMuted(_ muted: Bool) {
        audio.setMuted(muted)
        if muted, case .dropped = events?.yield(.end) { events?.finish(throwing: LiveFailure.backlog) }
    }

    func play(_ chunk: LivePCM) throws {
        guard chunk.rate > 0, chunk.rate <= 192000, !chunk.data.isEmpty, chunk.data.count.isMultiple(of: 2),
              let format = AVAudioFormat(standardFormatWithSampleRate: Double(chunk.rate), channels: 1),
              let source = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(chunk.data.count / 2)),
              let samples = source.floatChannelData?[0] else { throw VoiceFailure.audio }
        source.frameLength = source.frameCapacity
        chunk.data.withUnsafeBytes { bytes in
            for index in 0..<Int(source.frameLength) {
                samples[index] = Float(Int16(littleEndian: bytes.loadUnaligned(fromByteOffset: index * 2, as: Int16.self))) / 32768
            }
        }
        if converter?.inputFormat != format { converter = AVAudioConverter(from: format, to: audio.format) }
        guard let converter else { throw VoiceFailure.audio }
        let result = try convert(source, using: converter)
        guard result.frameLength > 0 else { return }
        let seconds = Double(result.frameLength) / audio.format.sampleRate
        guard duration + seconds <= 30 else { throw LiveFailure.backlog }
        duration += seconds
        buffers += 1
        let current = token
        audio.player.scheduleBuffer(result, completionCallbackType: .dataPlayedBack) { @Sendable [weak self] _ in
            Task { @MainActor in
                guard let self, self.token == current else { return }
                self.buffers -= 1
                self.duration = max(0, self.duration - seconds)
                if self.buffers == 0 { self.drained?() }
            }
        }
        try audio.player.playAudio()
    }

    func interrupt() {
        token = UUID()
        audio.player.stop()
        converter = nil
        buffers = 0
        duration = 0
    }

    func stop() {
        interrupt()
        audio.stop()
        events?.finish()
        events = nil
    }

    private func convert(_ source: AVAudioPCMBuffer, using converter: AVAudioConverter) throws -> AVAudioPCMBuffer {
        let capacity = AVAudioFrameCount(Double(source.frameLength) * audio.format.sampleRate / source.format.sampleRate + 64)
        guard let result = AVAudioPCMBuffer(pcmFormat: audio.format, frameCapacity: capacity) else { throw VoiceFailure.audio }
        var supplied = false
        var error: NSError?
        let status = converter.convert(to: result, error: &error) { _, inputStatus in
            if supplied { inputStatus.pointee = .noDataNow; return nil }
            supplied = true
            inputStatus.pointee = .haveData
            return source
        }
        if let error { throw error }
        guard status != .error else { throw VoiceFailure.audio }
        return result
    }
}

// The audio tap owns the converter and calls it serially; only PCM bytes leave this callback.
nonisolated private final class LiveCapture: AudioCapture, @unchecked Sendable {
    private let rate: Int
    private let events: AsyncThrowingStream<MicEvent, Error>.Continuation
    private var converter: AVAudioConverter?

    init(rate: Int, events: AsyncThrowingStream<MicEvent, Error>.Continuation) {
        self.rate = rate
        self.events = events
    }

    func consume(_ buffer: AVAudioPCMBuffer, time: AVAudioTime) {
        guard buffer.frameLength > 0 else { return }
        do {
            guard let format = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: Double(rate),
                                            channels: 1, interleaved: false) else { throw VoiceFailure.audio }
            if converter == nil { converter = AVAudioConverter(from: buffer.format, to: format) }
            guard let converter else { throw VoiceFailure.audio }
            let capacity = AVAudioFrameCount(Double(buffer.frameLength) * Double(rate) / buffer.format.sampleRate + 64)
            guard let result = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: capacity),
                  let retained = AVAudioPCMBuffer(pcmFormat: buffer.format, frameCapacity: buffer.frameLength) else {
                throw VoiceFailure.audio
            }
            retained.frameLength = buffer.frameLength
            let source = UnsafeMutableAudioBufferListPointer(buffer.mutableAudioBufferList)
            let target = UnsafeMutableAudioBufferListPointer(retained.mutableAudioBufferList)
            for index in source.indices {
                guard let input = source[index].mData, let output = target[index].mData else { throw VoiceFailure.audio }
                memcpy(output, input, Int(min(source[index].mDataByteSize, target[index].mDataByteSize)))
            }
            var supplied = false
            var error: NSError?
            let status = converter.convert(to: result, error: &error) { _, inputStatus in
                if supplied { inputStatus.pointee = .noDataNow; return nil }
                supplied = true
                inputStatus.pointee = .haveData
                return retained
            }
            if let error { throw error }
            guard status != .error, let samples = result.int16ChannelData?[0] else { throw VoiceFailure.audio }
            guard result.frameLength > 0 else { return }
            var sum: Float = 0
            for index in 0..<Int(result.frameLength) {
                let value = Float(samples[index]) / 32768
                sum += value * value
            }
            let level = min(1, sqrt(sum / Float(result.frameLength)) * 12)
            let chunk = LivePCM(data: Data(bytes: samples, count: Int(result.frameLength) * 2), rate: rate)
            if case .dropped = events.yield(.frame(chunk, level: level)) { throw LiveFailure.backlog }
        } catch { events.finish(throwing: error) }
    }
}
