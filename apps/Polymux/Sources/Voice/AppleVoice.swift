import AVFoundation
import Speech

nonisolated protocol AudioCapture: Sendable {
    func consume(_ buffer: AVAudioPCMBuffer, time: AVAudioTime)
}

@MainActor
final class VoiceAudio {
    let engine = AVAudioEngine()
    let player = AVAudioPlayerNode()
    let format = AVAudioFormat(standardFormatWithSampleRate: 48_000, channels: 1)!
    private let duplex: Bool
    private var tapped = false
    private var muted = false

    init(duplex: Bool = true) {
        self.duplex = duplex
        if duplex { engine.attach(player) }
    }

    func start(capture: any AudioCapture) throws {
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        if duplex {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetoothHFP])
        } else {
            try session.setCategory(.record, mode: .measurement, options: [.allowBluetoothHFP])
        }
        try session.setActive(true)
        #endif

        let input = engine.inputNode
        if duplex {
            do {
                try input.setVoiceProcessingEnabled(true)
                input.isVoiceProcessingInputMuted = muted
            } catch {
                throw VoiceFailure.echo
            }
        }
        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0 else {
            throw VoiceFailure.audio
        }
        if duplex {
            try engine.connectNode(player, to: engine.mainMixerNode, format: format)
            try engine.connectNode(engine.mainMixerNode, to: engine.outputNode, format: format)
        }
        try input.__installTap(onBus: 0, bufferSize: 1024, format: duplex ? format : inputFormat, error: ()) { @Sendable buffer, time in
            capture.consume(buffer, time: time)
        }
        tapped = true
        engine.prepare()
        try engine.start()
    }

    /// Restarts the engine after the system stopped it.
    func resume() throws {
        #if os(iOS)
        try AVAudioSession.sharedInstance().setActive(true)
        #endif
        if !engine.isRunning { try engine.start() }
    }

    func setSilenced(_ value: Bool) {
        engine.mainMixerNode.outputVolume = value ? 0 : 1
    }

    func setMuted(_ value: Bool) {
        muted = value
        if engine.isRunning, engine.inputNode.isVoiceProcessingEnabled {
            engine.inputNode.isVoiceProcessingInputMuted = value
        }
    }

    func stop() {
        if duplex { player.stop() }
        engine.stop()
        muted = false
        if tapped {
            engine.inputNode.removeTap(onBus: 0)
            tapped = false
        }
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
    }
}

// The audio tap calls this serially; the converter may retain input audio.
nonisolated final class VoiceCapture: AudioCapture, @unchecked Sendable {
    private let converter: AnalyzerInputConverter?
    private let inputs: AsyncStream<AnalyzerInput>.Continuation?
    private let buffers: AsyncStream<VoiceBuffer>.Continuation?
    private let events: AsyncThrowingStream<VoiceEvent, Error>.Continuation
    private let clock = NSLock()
    private var elapsed: Double = 0
    private var meterTime = Date.distantPast

    init(
        format: AVAudioFormat,
        inputs: AsyncStream<AnalyzerInput>.Continuation,
        events: AsyncThrowingStream<VoiceEvent, Error>.Continuation
    ) {
        converter = AnalyzerInputConverter(analyzerFormat: format)
        self.inputs = inputs
        buffers = nil
        self.events = events
    }

    fileprivate init(
        buffers: AsyncStream<VoiceBuffer>.Continuation,
        events: AsyncThrowingStream<VoiceEvent, Error>.Continuation
    ) {
        converter = nil
        inputs = nil
        self.buffers = buffers
        self.events = events
    }

    var time: Double { clock.withLock { elapsed } }

    func consume(_ buffer: AVAudioPCMBuffer, time: AVAudioTime) {
        guard buffer.frameLength > 0 else { return }
        do {
            let retained = try copy(buffer)
            let converted = try converter?.convert(retained, at: time) ?? []
            for input in converted {
                inputs?.yield(input)
            }
            if let buffers {
                let start = clock.withLock {
                    let start = elapsed
                    elapsed += Double(buffer.frameLength) / buffer.format.sampleRate
                    return start
                }
                buffers.yield(VoiceBuffer(value: retained, start: start))
            }
        } catch {
            events.finish(throwing: error)
            return
        }
        let now = Date()
        guard now.timeIntervalSince(meterTime) > 0.04 else { return }
        meterTime = now
        guard let samples = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return }
        var sum: Float = 0
        for index in 0..<Int(buffer.frameLength) {
            sum += samples[index] * samples[index]
        }
        let rms = sqrt(sum / Float(buffer.frameLength))
        events.yield(.level(min(1, rms * 12)))
    }

    private func copy(_ buffer: AVAudioPCMBuffer) throws -> AVAudioPCMBuffer {
        guard let retained = AVAudioPCMBuffer(pcmFormat: buffer.format, frameCapacity: buffer.frameLength) else {
            throw VoiceFailure.audio
        }
        retained.frameLength = buffer.frameLength
        let source = UnsafeMutableAudioBufferListPointer(buffer.mutableAudioBufferList)
        let target = UnsafeMutableAudioBufferListPointer(retained.mutableAudioBufferList)
        for index in source.indices {
            guard let input = source[index].mData, let output = target[index].mData else { throw VoiceFailure.audio }
            memcpy(output, input, Int(min(source[index].mDataByteSize, target[index].mDataByteSize)))
        }
        return retained
    }

}

@MainActor
final class AppleInput: SpeechInput {
    private let audio: VoiceAudio
    private var analyzer: SpeechAnalyzer?
    private var inputs: AsyncStream<AnalyzerInput>.Continuation?
    private var events: AsyncThrowingStream<VoiceEvent, Error>.Continuation?
    private var tasks: [Task<Void, Never>] = []
    private var locale: Locale?
    private var fallback: LegacyInput?

    init(audio: VoiceAudio) { self.audio = audio }

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        let microphone = await AVAudioApplication.requestRecordPermission()
        guard microphone else { throw VoiceFailure.microphone }
        let permission = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { @Sendable status in
                continuation.resume(returning: status)
            }
        }
        guard permission == .authorized else { throw VoiceFailure.recognition }
        guard SpeechTranscriber.isAvailable,
              let selected = await SpeechTranscriber.supportedLocale(equivalentTo: .current) else {
            let fallback = LegacyInput(audio: audio)
            self.fallback = fallback
            return try await fallback.start()
        }
        let transcriber = SpeechTranscriber(locale: selected, preset: .progressiveTranscription)
        let detector = SpeechDetector(detectionOptions: .init(sensitivityLevel: .medium), reportResults: true)
        let modules: [any SpeechModule] = [transcriber, detector]
        if try await AssetInventory.reserve(locale: selected) { locale = selected }
        if let installation = try await AssetInventory.assetInstallationRequest(supporting: modules) {
            try await installation.downloadAndInstall()
        }
        try Task.checkCancellation()
        guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: modules) else {
            throw VoiceFailure.audio
        }
        let analyzer = SpeechAnalyzer(modules: modules)
        self.analyzer = analyzer
        let context = AnalysisContext()
        context.contextualStrings[.general] = ["Polymux"]
        try await analyzer.setContext(context)
        try await analyzer.prepareToAnalyze(in: format)
        try Task.checkCancellation()

        let inputStream = AsyncStream<AnalyzerInput>.makeStream(bufferingPolicy: .bufferingNewest(128))
        let eventStream = AsyncThrowingStream<VoiceEvent, Error>.makeStream(bufferingPolicy: .bufferingNewest(128))
        inputs = inputStream.continuation
        events = eventStream.continuation
        let capture = VoiceCapture(format: format, inputs: inputStream.continuation, events: eventStream.continuation)
        tasks = [
            Task {
                do {
                    for try await result in transcriber.results {
                        eventStream.continuation.yield(.transcript(.init(
                            text: String(result.text.characters),
                            start: result.range.start.seconds,
                            end: result.range.end.seconds,
                            final: result.isFinal
                        )))
                    }
                } catch {
                    if !Task.isCancelled { eventStream.continuation.finish(throwing: error) }
                }
            },
            Task {
                do {
                    for try await result in detector.results {
                        eventStream.continuation.yield(.activity(result.speechDetected))
                    }
                } catch {
                    if !Task.isCancelled { eventStream.continuation.finish(throwing: error) }
                }
            },
            Task {
                do {
                    try await analyzer.start(inputSequence: inputStream.stream)
                } catch {
                    if !Task.isCancelled { eventStream.continuation.finish(throwing: error) }
                }
            }
        ]
        try audio.start(capture: capture)
        return eventStream.stream
    }

    func setMuted(_ muted: Bool) {
        if let fallback { fallback.setMuted(muted) }
        else { audio.setMuted(muted) }
    }

    func finalize() async throws {
        if let fallback { try await fallback.finalize() }
        else { try await analyzer?.finalize(through: nil) }
    }

    func stop() async {
        await fallback?.stop()
        fallback = nil
        audio.stop()
        inputs?.finish()
        events?.finish()
        for task in tasks { task.cancel() }
        tasks = []
        await analyzer?.cancelAndFinishNow()
        analyzer = nil
        inputs = nil
        events = nil
        if let locale { await AssetInventory.release(reservedLocale: locale) }
        locale = nil
    }
}

private nonisolated struct VoiceBuffer: @unchecked Sendable {
    let value: AVAudioPCMBuffer
    let start: Double
}

@MainActor
private final class LegacyInput: SpeechInput {
    private let audio: VoiceAudio
    private var recognizer: SFSpeechRecognizer?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var recognition: SFSpeechRecognitionTask?
    private var capture: VoiceCapture?
    private var buffers: AsyncStream<VoiceBuffer>.Continuation?
    private var events: AsyncThrowingStream<VoiceEvent, Error>.Continuation?
    private var inputTask: Task<Void, Never>?
    private var renewal: Task<Void, Never>?
    private var token = UUID()
    private var boundary: Double = 0
    private var onDevice = true
    private var received = false
    private var replay: [VoiceBuffer] = []
    private var canReplay = true

    init(audio: VoiceAudio) { self.audio = audio }

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        guard let recognizer = SFSpeechRecognizer(locale: .current) else { throw VoiceFailure.locale }
        guard recognizer.isAvailable else { throw VoiceFailure.unavailable }
        self.recognizer = recognizer
        let bufferStream = AsyncStream<VoiceBuffer>.makeStream(bufferingPolicy: .bufferingNewest(128))
        let eventStream = AsyncThrowingStream<VoiceEvent, Error>.makeStream(bufferingPolicy: .bufferingNewest(128))
        buffers = bufferStream.continuation
        events = eventStream.continuation
        let capture = VoiceCapture(buffers: bufferStream.continuation, events: eventStream.continuation)
        self.capture = capture
        finishTurn()
        inputTask = Task { [weak self] in
            for await buffer in bufferStream.stream {
                guard let self, !Task.isCancelled else { return }
                guard buffer.start >= self.boundary else { continue }
                if self.request?.requiresOnDeviceRecognition == true, !self.received, self.canReplay {
                    self.replay.append(buffer)
                    if buffer.start - (self.replay.first?.start ?? buffer.start) > 30 {
                        self.canReplay = false
                        self.replay = []
                    }
                }
                self.request?.append(buffer.value)
            }
        }
        try audio.start(capture: capture)
        return eventStream.stream
    }

    private func finishTurn() {
        recognize(at: capture?.time ?? 0)
    }

    func finalize() async throws { request?.endAudio() }

    private func recognize(at start: Double, replay: [VoiceBuffer] = []) {
        guard let recognizer, let events else { return }
        token = UUID()
        renewal?.cancel()
        recognition?.cancel()
        request?.endAudio()
        boundary = start
        received = false
        self.replay = replay
        canReplay = true
        let current = token
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.requiresOnDeviceRecognition = onDevice && recognizer.supportsOnDeviceRecognition
        let local = request.requiresOnDeviceRecognition
        request.contextualStrings = ["Polymux"]
        request.taskHint = .dictation
        request.addsPunctuation = true
        self.request = request
        recognition = recognizer.recognitionTask(with: request) { @Sendable [weak self] result, error in
            let transcript = result.map { result in
                let last = result.bestTranscription.segments.last
                return VoiceTranscript(
                    text: result.bestTranscription.formattedString,
                    start: start,
                    end: start + max(0.01, (last?.timestamp ?? 0) + (last?.duration ?? 0)),
                    final: result.isFinal
                )
            }
            Task { @MainActor in
                guard let self, self.token == current else { return }
                if let transcript {
                    if !transcript.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        self.received = true
                        self.replay = []
                    }
                    events.yield(.transcript(transcript))
                    if transcript.final { self.finishTurn(); return }
                }
                if let error {
                    let failure = error as NSError
                    if local, !self.received, self.canReplay, failure.domain == "kLSRErrorDomain", failure.code == 300 {
                        self.onDevice = false
                        self.recognize(at: start, replay: self.replay)
                    } else if failure.domain == "kAFAssistantErrorDomain", failure.code == 1110 {
                        self.finishTurn()
                    } else {
                        events.finish(throwing: VoiceFailure.speech)
                    }
                }
            }
        }
        for buffer in replay { request.append(buffer.value) }
        renewal = Task { [weak self] in
            try? await Task.sleep(for: .seconds(55))
            guard !Task.isCancelled, self?.token == current else { return }
            self?.finishTurn()
        }
    }

    func setMuted(_ muted: Bool) {
        audio.setMuted(muted)
        finishTurn()
    }

    func stop() async {
        audio.stop()
        token = UUID()
        renewal?.cancel()
        inputTask?.cancel()
        recognition?.cancel()
        request?.endAudio()
        buffers?.finish()
        events?.finish()
        renewal = nil
        inputTask = nil
        recognition = nil
        request = nil
        recognizer = nil
        capture = nil
        buffers = nil
        events = nil
        replay = []
    }
}


