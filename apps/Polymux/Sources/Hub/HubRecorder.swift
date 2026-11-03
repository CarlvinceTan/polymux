import AVFoundation
import Foundation
import Observation

/// One voice note. It stays in a temporary file until it's sent or discarded.
@MainActor @Observable
final class HubRecorder {
    enum Phase { case idle, recording, paused }

    private(set) var phase = Phase.idle
    private(set) var seconds = 0.0
    private(set) var level: Float = 0
    @ObservationIgnored private var recorder: AVAudioRecorder?
    @ObservationIgnored private var meter: Task<Void, Never>?
    #if DEBUG
    /// Stands in for the microphone, which fixture flows can't be granted.
    @ObservationIgnored private var sample: URL?
    #endif

    func start() async {
        #if DEBUG
        if VerifyFixture.enabled {
            let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
            try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let file = folder.appending(path: "Voice message.m4a")
            try? Data([0]).write(to: file)
            sample = file
            seconds = 3
            phase = .recording
            return
        }
        #endif
        guard phase == .idle, await AVAudioApplication.requestRecordPermission() else { return }
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setCategory(.playAndRecord, options: [.defaultToSpeaker, .allowBluetoothHFP])
        try? AVAudioSession.sharedInstance().setActive(true)
        #endif
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let format: [String: Any] = [AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 24_000, AVNumberOfChannelsKey: 1, AVEncoderBitRateKey: 32_000]
        guard let take = try? AVAudioRecorder(url: folder.appending(path: "Voice message.m4a"), settings: format) else { return }
        take.isMeteringEnabled = true
        guard take.record() else { return }
        recorder = take
        seconds = 0
        phase = .recording
        follow()
    }

    private func follow() {
        meter = Task { [weak self] in
            while !Task.isCancelled, let self, let recorder = self.recorder {
                recorder.updateMeters()
                self.level = pow(10, recorder.averagePower(forChannel: 0) / 20)
                self.seconds = recorder.currentTime
                try? await Task.sleep(for: .milliseconds(100))
            }
        }
    }

    func toggle() {
        #if DEBUG
        if sample != nil {
            phase = phase == .recording ? .paused : .recording
            return
        }
        #endif
        guard let recorder else { return }
        if phase == .recording {
            recorder.pause()
            meter?.cancel()
            level = 0
            phase = .paused
        } else if recorder.record() {
            phase = .recording
            follow()
        }
    }

    private func stop() -> URL? {
        #if DEBUG
        if let sample {
            self.sample = nil
            phase = .idle
            return sample
        }
        #endif
        meter?.cancel()
        recorder?.stop()
        let file = recorder?.url
        recorder = nil
        level = 0
        phase = .idle
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
        return file
    }

    /// Ends the take; the caller removes its folder once it's sent.
    func finish() -> (file: URL, duration: Int64)? {
        let elapsed = max(seconds, recorder?.currentTime ?? 0)
        guard let file = stop(), elapsed > 0 else { return nil }
        return (file, Int64(elapsed * 1000))
    }

    func discard() {
        if let file = stop() { try? FileManager.default.removeItem(at: file.deletingLastPathComponent()) }
    }
}
