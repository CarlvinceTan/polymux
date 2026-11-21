import AVFoundation
import Speech

public protocol AudioCapture: Sendable {
    func consume(_ buffer: AVAudioPCMBuffer, time: AVAudioTime)
}

@MainActor
public final class VoiceAudio {
    public let engine = AVAudioEngine()
    public let player = AVAudioPlayerNode()
    public let format = AVAudioFormat(standardFormatWithSampleRate: 48_000, channels: 1)!
    private let duplex: Bool
    private var tapped = false
    private var muted = false
    #if os(iOS)
    private var sessionActive = false
    #endif

    public init(duplex: Bool = true) {
        self.duplex = duplex
        if duplex { engine.attach(player) }
    }

    public func start(capture: any AudioCapture) throws {
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        if duplex {
            try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetoothHFP])
        } else {
            try session.setCategory(.record, mode: .measurement, options: [.allowBluetoothHFP])
        }
        try session.setActive(true)
        sessionActive = true
        #endif

        let input = engine.inputNode
        if duplex {
            do {
                try input.setVoiceProcessingEnabled(true)
                input.isVoiceProcessingInputMuted = muted
            } catch {
                throw SpeechFailure.echo
            }
        }
        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0 else {
            throw SpeechFailure.audio
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
    public func resume() throws {
        #if os(iOS)
        try AVAudioSession.sharedInstance().setActive(true)
        sessionActive = true
        #endif
        if !engine.isRunning { try engine.start() }
    }

    public func setSilenced(_ value: Bool) {
        engine.mainMixerNode.outputVolume = value ? 0 : 1
    }

    public func setMuted(_ value: Bool) {
        muted = value
        if engine.isRunning, engine.inputNode.isVoiceProcessingEnabled {
            engine.inputNode.isVoiceProcessingInputMuted = value
        }
    }

    public func stop() {
        if duplex { player.stop() }
        engine.stop()
        muted = false
        if tapped {
            engine.inputNode.removeTap(onBus: 0)
            tapped = false
        }
        #if os(iOS)
        if sessionActive {
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            sessionActive = false
        }
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
    // The tap calls these serially, so they need no lock.
    private var cap: PauseCap?
    private var tapFormat: AVAudioFormat?

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
        reportLevel(buffer)
        do {
            guard let kept = try capped(try copy(buffer)) else { return }
            let converted = try converter?.convert(kept, at: time) ?? []
            for input in converted {
                inputs?.yield(input)
            }
            if let buffers {
                let start = clock.withLock {
                    let start = elapsed
                    elapsed += Double(kept.frameLength) / kept.format.sampleRate
                    return start
                }
                buffers.yield(VoiceBuffer(value: kept, start: start))
            }
        } catch {
            events.finish(throwing: error)
        }
    }

    /// Long pauses leave the audio the recogniser hears; see PauseCap.
    private func capped(_ buffer: AVAudioPCMBuffer) throws -> AVAudioPCMBuffer? {
        guard buffer.format.channelCount == 1, buffer.format.commonFormat == .pcmFormatFloat32,
              let input = buffer.floatChannelData?[0] else { return buffer }
        if cap == nil { cap = PauseCap(sampleRate: Int(buffer.format.sampleRate)) }
        if tapFormat == nil { tapFormat = buffer.format }
        let samples = Array(UnsafeBufferPointer(start: input, count: Int(buffer.frameLength)))
        let emitted = cap!.process(samples)
        guard !emitted.isEmpty else { return nil }
        guard let out = AVAudioPCMBuffer(pcmFormat: buffer.format, frameCapacity: AVAudioFrameCount(emitted.count)),
              let destination = out.floatChannelData?[0] else { return buffer }
        out.frameLength = AVAudioFrameCount(emitted.count)
        emitted.withUnsafeBufferPointer { source in
            _ = memcpy(destination, source.baseAddress!, source.count * MemoryLayout<Float>.size)
        }
        return out
    }

    /// Feeds trailing quiet so the recogniser closes its last sentence before finalizing.
    func emitSilence(seconds: Double) {
        guard let converter, let format = tapFormat, let inputs else { return }
        let frames = AVAudioFrameCount(format.sampleRate * seconds)
        guard frames > 0, let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: frames),
              let samples = buffer.floatChannelData?[0] else { return }
        buffer.frameLength = frames
        memset(samples, 0, Int(frames) * MemoryLayout<Float>.size)
        let time = AVAudioTime(sampleTime: 0, atRate: format.sampleRate)
        for input in (try? converter.convert(buffer, at: time)) ?? [] {
            inputs.yield(input)
        }
    }

    private func reportLevel(_ buffer: AVAudioPCMBuffer) {
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
            throw SpeechFailure.audio
        }
        retained.frameLength = buffer.frameLength
        let source = UnsafeMutableAudioBufferListPointer(buffer.mutableAudioBufferList)
        let target = UnsafeMutableAudioBufferListPointer(retained.mutableAudioBufferList)
        for index in source.indices {
            guard let input = source[index].mData, let output = target[index].mData else { throw SpeechFailure.audio }
            memcpy(output, input, Int(min(source[index].mDataByteSize, target[index].mDataByteSize)))
        }
        return retained
    }

}

@MainActor
final class AppleInput: SpeechInput {
    private let audio: VoiceAudio
    private let language: DictationLanguage
    private var analyzer: SpeechAnalyzer?
    private var inputs: AsyncStream<AnalyzerInput>.Continuation?
    private var events: AsyncThrowingStream<VoiceEvent, Error>.Continuation?
    private var tasks: [Task<Void, Never>] = []
    private var coordinator: DictationCoordinator?
    private var reserved: [Locale] = []
    private var fallback: LegacyInput?
    private var capture: VoiceCapture?
    private var terms = Vocabulary.terms(Vocabulary.base)

    init(audio: VoiceAudio, language: DictationLanguage = .multilingual) {
        self.audio = audio
        self.language = language
    }

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        let microphone = await AVAudioApplication.requestRecordPermission()
        try Task.checkCancellation()
        guard microphone else { throw SpeechFailure.microphone }
        let permission = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { @Sendable status in
                continuation.resume(returning: status)
            }
        }
        try Task.checkCancellation()
        guard permission == .authorized else { throw SpeechFailure.recognition }
        guard SpeechTranscriber.isAvailable else { return try await legacy() }

        var ready: [(Locale, DictationTranscriber)] = []
        for locale in DictationDevice.locales(for: DictationDevice.language(language)) {
            guard let selected = await DictationTranscriber.supportedLocale(equivalentTo: locale),
                  !ready.contains(where: { $0.0.identifier == selected.identifier }) else { continue }
            let transcriber = DictationTranscriber(
                locale: selected,
                contentHints: [],
                transcriptionOptions: [.punctuation],
                reportingOptions: [.volatileResults, .frequentFinalization],
                attributeOptions: [.transcriptionConfidence]
            )
            ready.append((selected, transcriber))
        }
        guard !ready.isEmpty else { return try await legacy() }

        do {
            for (locale, _) in ready {
                if try await AssetInventory.reserve(locale: locale) { reserved.append(locale) }
            }
            if let installation = try await AssetInventory.assetInstallationRequest(supporting: ready.map(\.1)) {
                try await installation.downloadAndInstall()
            }
        } catch {
            var installed: [(Locale, DictationTranscriber)] = []
            for (locale, transcriber) in ready {
                do {
                    if let installation = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
                        try await installation.downloadAndInstall()
                    }
                    installed.append((locale, transcriber))
                } catch { continue }
            }
            ready = installed
            if ready.isEmpty {
                await releaseReserved()
                return try await legacy()
            }
        }
        try Task.checkCancellation()

        let modules = ready.map(\.1)
        guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: modules) else {
            await releaseReserved()
            throw SpeechFailure.audio
        }
        let analyzer = SpeechAnalyzer(modules: modules)
        self.analyzer = analyzer
        let context = AnalysisContext()
        context.contextualStrings[.general] = terms
        try await analyzer.setContext(context)
        try await analyzer.prepareToAnalyze(in: format)
        try Task.checkCancellation()

        let inputStream = AsyncStream<AnalyzerInput>.makeStream(bufferingPolicy: .bufferingNewest(128))
        let eventStream = AsyncThrowingStream<VoiceEvent, Error>.makeStream(bufferingPolicy: .bufferingNewest(128))
        inputs = inputStream.continuation
        events = eventStream.continuation
        let capture = VoiceCapture(format: format, inputs: inputStream.continuation, events: eventStream.continuation)
        self.capture = capture
        let coordinator = DictationCoordinator(continuation: eventStream.continuation,
                                               locales: ready.map { $0.0.identifier })
        self.coordinator = coordinator
        tasks = ready.map { (locale, transcriber) in
            Task {
                do {
                    for try await result in transcriber.results {
                        await coordinator.accept(result, locale: locale.identifier)
                    }
                } catch {
                    if !Task.isCancelled { await coordinator.failed(locale.identifier) }
                }
            }
        }
        tasks.append(Task {
            do {
                try await analyzer.start(inputSequence: inputStream.stream)
            } catch {
                if !Task.isCancelled { eventStream.continuation.finish(throwing: error) }
            }
        })
        try audio.start(capture: capture)
        return eventStream.stream
    }

    private func legacy() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        let fallback = LegacyInput(audio: audio, language: language)
        self.fallback = fallback
        await fallback.setVocabulary(terms)
        return try await fallback.start()
    }

    func setMuted(_ muted: Bool) {
        if let fallback { fallback.setMuted(muted) }
        else { audio.setMuted(muted) }
    }

    func finalize() async throws {
        if let fallback { try await fallback.finalize() }
        else {
            let listeners = tasks
            capture?.emitSilence(seconds: 0.4)
            inputs?.finish()
            do {
                try await analyzer?.finalizeAndFinishThroughEndOfInput()
            } catch {
                await coordinator?.finish()
                throw error
            }
            for listener in listeners { await listener.value }
            await coordinator?.finish()
        }
    }

    func setVocabulary(_ terms: [String]) async {
        self.terms = terms
        if let analyzer {
            let context = AnalysisContext()
            context.contextualStrings[.general] = terms
            try? await analyzer.setContext(context)
        }
    }

    func stop() async {
        await coordinator?.finish()
        coordinator = nil
        await fallback?.stop()
        fallback = nil
        capture = nil
        audio.stop()
        inputs?.finish()
        events?.finish()
        for task in tasks { task.cancel() }
        tasks = []
        await analyzer?.cancelAndFinishNow()
        analyzer = nil
        inputs = nil
        events = nil
        await releaseReserved()
    }

    private func releaseReserved() async {
        let locales = reserved
        reserved = []
        for locale in locales { await AssetInventory.release(reservedLocale: locale) }
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
    private var ending = false
    private var terms = Vocabulary.terms(Vocabulary.base)
    private let language: DictationLanguage

    init(audio: VoiceAudio, language: DictationLanguage = .multilingual) {
        self.audio = audio
        self.language = language
    }

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        var locale = DictationDevice.locale(for: language)
        var recognizer = SFSpeechRecognizer(locale: locale)
        if recognizer == nil, language == .multilingual {
            locale = DictationDevice.locale(for: .english)
            recognizer = SFSpeechRecognizer(locale: locale)
        }
        guard let recognizer else { throw SpeechFailure.locale }
        guard recognizer.isAvailable else { throw SpeechFailure.unavailable }
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

    func finalize() async throws {
        ending = true
        renewal?.cancel()
        buffers?.finish()
        await inputTask?.value
        request?.endAudio()
    }

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
        request.contextualStrings = terms
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
                    final: result.isFinal,
                    confidences: result.isFinal ? result.bestTranscription.segments.map { Double($0.confidence) } : []
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
                    if transcript.final {
                        if self.ending { events.finish() }
                        else { self.finishTurn() }
                        return
                    }
                }
                if let error {
                    let failure = error as NSError
                    if self.ending {
                        events.finish()
                    } else if local, !self.received, self.canReplay, failure.domain == "kLSRErrorDomain", failure.code == 300 {
                        self.onDevice = false
                        self.recognize(at: start, replay: self.replay)
                    } else if failure.domain == "kAFAssistantErrorDomain", failure.code == 1110 {
                        self.finishTurn()
                    } else {
                        events.finish(throwing: SpeechFailure.speech)
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

    func setVocabulary(_ terms: [String]) async {
        self.terms = terms
        request?.contextualStrings = terms
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
