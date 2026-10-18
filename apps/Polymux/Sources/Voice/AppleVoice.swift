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
    private var tapped = false
    private var muted = false

    init() {
        engine.attach(player)
    }

    func start(capture: any AudioCapture) throws {
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetoothHFP])
        try session.setActive(true)
        #endif

        let input = engine.inputNode
        do {
            try input.setVoiceProcessingEnabled(true)
            input.isVoiceProcessingInputMuted = muted
        } catch {
            throw VoiceFailure.echo(error.localizedDescription)
        }
        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0 else {
            throw VoiceFailure.audio
        }
        try engine.connectNode(player, to: engine.mainMixerNode, format: format)
        try engine.connectNode(engine.mainMixerNode, to: engine.outputNode, format: format)
        try input.__installTap(onBus: 0, bufferSize: 1024, format: format, error: ()) { @Sendable buffer, time in
            capture.consume(buffer, time: time)
        }
        tapped = true
        engine.prepare()
        try engine.start()
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
        player.stop()
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

    func finishTurn() { fallback?.finishTurn() }

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

    func finishTurn() {
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

private nonisolated struct SpeechBuffer: @unchecked Sendable {
    let value: AVAudioPCMBuffer
}

@MainActor
final class AppleOutput: NSObject, SpeechOutput {
    var firstAudio: (() -> Void)?
    private let audio: VoiceAudio
    private let synthesizer = AVSpeechSynthesizer()
    private var completion: CheckedContinuation<Void, Error>?
    private var token = UUID()
    private var converter: AVAudioConverter?
    private var buffers = 0
    private var finished = false
    private var started = false
    private var timeout: Task<Void, Never>?

    init(audio: VoiceAudio) { self.audio = audio }

    func setSilenced(_ silenced: Bool) { audio.setSilenced(silenced) }

    func speak(_ text: String) async throws {
        try Task.checkCancellation()
        let current = UUID()
        token = current
        converter = nil
        buffers = 0
        finished = false
        started = false
        let utterance = AVSpeechUtterance(string: text)
        utterance.voice = Self.voice(for: .current)
        utterance.rate = AVSpeechUtteranceDefaultSpeechRate
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                completion = continuation
                synthesizer.write(utterance) { @Sendable [weak self] buffer in
                    guard let buffer = buffer as? AVAudioPCMBuffer else { return }
                    let retained = SpeechBuffer(value: buffer)
                    Task { @MainActor in self?.receive(retained.value, token: current) }
                }
                timeout = Task { [weak self] in
                    try? await Task.sleep(for: .seconds(30))
                    guard !Task.isCancelled, self?.token == current else { return }
                    self?.finish(throwing: VoiceFailure.audio)
                }
            }
        } onCancel: {
            Task { @MainActor [weak self] in
                if self?.token == current { self?.stop() }
            }
        }
    }

    func stop() {
        token = UUID()
        synthesizer.stopSpeaking(at: .immediate)
        audio.player.stop()
        finish(throwing: CancellationError())
    }

    private static func voice(for locale: Locale) -> AVSpeechSynthesisVoice? {
        if locale.language.languageCode?.identifier == "en",
           let australian = AVSpeechSynthesisVoice(identifier: "com.apple.siri.natural.Catherine") {
            return australian
        }
        let preferred = AVSpeechSynthesisVoice(language: locale.identifier)
            ?? AVSpeechSynthesisVoice(language: "en-US")
        let language = preferred?.language.split(separator: "-").first
        let voices = AVSpeechSynthesisVoice.speechVoices().filter {
            $0.language.split(separator: "-").first == language
                && !$0.voiceTraits.contains(.isNoveltyVoice)
                && !$0.voiceTraits.contains(.isPersonalVoice)
                && !$0.identifier.hasPrefix("com.apple.eloquence.")
        }
        return voices.max { left, right in
            if left.quality != right.quality { return left.quality.rawValue < right.quality.rawValue }
            if (left.language == preferred?.language) != (right.language == preferred?.language) {
                return right.language == preferred?.language
            }
            if (left.identifier == preferred?.identifier) != (right.identifier == preferred?.identifier) {
                return right.identifier == preferred?.identifier
            }
            return left.identifier > right.identifier
        } ?? preferred
    }

    private func receive(_ buffer: AVAudioPCMBuffer, token current: UUID) {
        guard token == current, completion != nil else { return }
        guard buffer.frameLength > 0 else {
            finished = true
            if buffers == 0 { finish() }
            return
        }
        do {
            let converted = try convert(buffer)
            buffers += 1
            audio.player.scheduleBuffer(converted, completionCallbackType: .dataPlayedBack) { @Sendable [weak self] _ in
                Task { @MainActor in
                    guard let self, self.token == current else { return }
                    self.buffers -= 1
                    if self.finished, self.buffers == 0 { self.finish() }
                }
            }
            if !started {
                started = true
                try audio.player.playAudio()
                firstAudio?()
            }
        } catch {
            audio.player.stop()
            finish(throwing: error)
        }
    }

    private func convert(_ buffer: AVAudioPCMBuffer) throws -> AVAudioPCMBuffer {
        if converter == nil { converter = AVAudioConverter(from: buffer.format, to: audio.format) }
        guard let converter else { throw VoiceFailure.audio }
        let ratio = audio.format.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio + 64)
        guard let result = AVAudioPCMBuffer(pcmFormat: audio.format, frameCapacity: capacity) else {
            throw VoiceFailure.audio
        }
        var supplied = false
        var error: NSError?
        let status = converter.convert(to: result, error: &error) { _, inputStatus in
            if supplied {
                inputStatus.pointee = .noDataNow
                return nil
            }
            supplied = true
            inputStatus.pointee = .haveData
            return buffer
        }
        if let error { throw error }
        guard status != .error else { throw VoiceFailure.audio }
        return result
    }

    private func finish(throwing error: Error? = nil) {
        timeout?.cancel()
        timeout = nil
        let continuation = completion
        completion = nil
        if let error { continuation?.resume(throwing: error) }
        else { continuation?.resume() }
    }
}
