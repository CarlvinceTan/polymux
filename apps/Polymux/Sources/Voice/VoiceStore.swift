import Foundation
import Observation
import PolymuxKit

@MainActor
@Observable
final class VoiceStore: VoiceSession {
    private(set) var phase = VoicePhase.preparing
    private(set) var agentID: String?
    private(set) var transcript = ""
    private(set) var response = ""
    private(set) var level: Float = 0
    private(set) var issue: String?
    private(set) var firstWord: Double?
    private(set) var firstAudio: Double?
    private(set) var levels = Array(repeating: Float(0), count: 36)
    private(set) var muted = false
    private(set) var silenced = false

    private let input: any SpeechInput
    private let output: any SpeechOutput
    private let reply: any VoiceReply
    private var inputTask: Task<Void, Never>?
    private var startup: Task<Void, Never>?
    private var shutdown: Task<Void, Never>?
    private var replyTask: Task<Void, Never>?
    private var speechTask: Task<Void, Error>?
    private var endpoint: Task<Void, Never>?
    private var interruption: Task<Void, Never>?
    private var segments: [Double: VoiceTranscript] = [:]
    private var consumedThrough = -Double.infinity
    private var activity = false
    private var audible = false
    private var finalizing = false
    private var soundAt = Date.distantPast
    private var speechAt: Date?
    private var suppressUntil = Date.distantPast
    private var submittedAt: Date?
    private var turn = UUID()
    private var closed = false
    private var session = UUID()

    init(input: any SpeechInput, output: any SpeechOutput, reply: any VoiceReply) {
        self.input = input
        self.output = output
        self.reply = reply
        output.firstAudio = { [weak self] in
            guard let self, self.firstAudio == nil, let submitted = self.submittedAt else { return }
            self.firstAudio = Date().timeIntervalSince(submitted)
        }
    }

    convenience init(reply: any VoiceReply) {
        let audio = VoiceAudio()
        self.init(input: AppleInput(audio: audio), output: AppleOutput(audio: audio), reply: reply)
    }

    func start() async {
        if let shutdown { await shutdown.value }
        guard !Task.isCancelled else { return }
        if let startup { await startup.value; return }
        guard inputTask == nil else { return }
        closed = false
        muted = false
        silenced = false
        input.setMuted(false)
        output.setSilenced(false)
        issue = nil
        transcript = ""
        segments = [:]
        consumedThrough = -Double.infinity
        activity = false
        audible = false
        finalizing = false
        soundAt = .distantPast
        speechAt = nil
        level = 0
        levels = Array(repeating: 0, count: 36)
        phase = .preparing
        agentID = nil
        let current = UUID()
        session = current
        let task = Task { [weak self] in
            guard let self else { return }
            await self.prepare(current)
        }
        startup = task
        await withTaskCancellationHandler {
            await task.value
        } onCancel: { task.cancel() }
        if session == current { startup = nil }
    }

    private func prepare(_ current: UUID) async {
        do {
            let events = try await input.start()
            guard session == current, !closed, !Task.isCancelled else {
                await input.stop()
                return
            }
            input.setMuted(muted)
            phase = .listening
            inputTask = Task { [weak self] in
                do {
                    for try await event in events {
                        guard !Task.isCancelled, self?.session == current, self?.closed == false else { return }
                        self?.receive(event)
                    }
                } catch {
                    guard !Task.isCancelled, self?.session == current, self?.closed == false else { return }
                    self?.fail(error)
                    await self?.stop()
                }
            }
        } catch {
            await input.stop()
            if session == current, !closed, !Task.isCancelled { fail(error) }
        }
    }

    func stop() async {
        if let shutdown { await shutdown.value; return }
        guard !closed else { return }
        closed = true
        phase = issue == nil ? .interrupted : .failed
        agentID = nil
        session = UUID()
        turn = UUID()
        endpoint?.cancel()
        finalizing = false
        interruption?.cancel()
        replyTask?.cancel()
        speechTask?.cancel()
        inputTask?.cancel()
        inputTask = nil
        let preparing = startup
        startup = nil
        preparing?.cancel()
        level = 0
        levels = Array(repeating: 0, count: 36)
        output.stop()
        let task = Task { [input, reply] in
            await preparing?.value
            await input.stop()
            await reply.stop()
        }
        shutdown = task
        await task.value
        shutdown = nil
    }

    func retry() async {
        await stop()
        guard !Task.isCancelled else { return }
        segments = [:]
        consumedThrough = -Double.infinity
        transcript = ""
        await start()
    }

    func toggleSilence() {
        guard !closed else { return }
        silenced.toggle()
        output.setSilenced(silenced)
    }

    func toggleMute() {
        guard !closed else { return }
        muted.toggle()
        input.setMuted(muted)
        if !finalizing { endpoint?.cancel() }
        consumedThrough = max(consumedThrough, segments.values.map(\.end).max() ?? consumedThrough)
        segments = [:]
        transcript = ""
        activity = false
        audible = false
        speechAt = nil
        level = 0
        levels = Array(repeating: 0, count: 36)
    }

    func interrupt() {
        guard phase == .thinking || phase == .speaking else { return }
        turn = UUID()
        replyTask?.cancel()
        speechTask?.cancel()
        output.stop()
        if segments.isEmpty { transcript = "" }
        phase = .interrupted
        agentID = nil
        interruption?.cancel()
        interruption = Task { [weak self] in
            guard let self else { return }
            await self.reply.stop()
            guard !Task.isCancelled, !self.closed else { return }
            self.phase = .listening
            if !self.segments.isEmpty { self.scheduleEnd() }
        }
    }

    func submit() {
        guard phase == .listening, !closed, !muted else { return }
        let text = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        if !finalizing { endpoint?.cancel() }
        consumedThrough = max(consumedThrough, segments.values.map(\.end).max() ?? consumedThrough)
        segments = [:]
        input.finishTurn()
        response = ""
        firstWord = nil
        firstAudio = nil
        submittedAt = Date()
        suppressUntil = Date().addingTimeInterval(0.25)
        phase = .thinking
        let current = UUID()
        turn = current
        let sentences = AsyncStream<VoiceFragment>.makeStream()
        speechTask = Task { [weak self] in
            guard let self else { return }
            for await sentence in sentences.stream {
                try Task.checkCancellation()
                guard self.turn == current else { throw CancellationError() }
                self.phase = .speaking
                self.agentID = sentence.agentID
                try await self.output.speak(sentence.text)
                guard self.turn == current, !self.closed else { throw CancellationError() }
                self.agentID = nil
                self.phase = .thinking
            }
        }
        replyTask = Task { [weak self] in
            guard let self else { return }
            var pending = ""
            var authorID: String?
            do {
                for try await fragment in self.reply.stream(text) {
                    try Task.checkCancellation()
                    guard self.turn == current else { throw CancellationError() }
                    if self.firstWord == nil, !fragment.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        self.firstWord = Date().timeIntervalSince(self.submittedAt ?? Date())
                    }
                    self.response += fragment.text
                    if authorID != fragment.agentID, !pending.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        sentences.continuation.yield(VoiceFragment(text: pending, agentID: authorID))
                        pending = ""
                    }
                    authorID = fragment.agentID
                    pending += fragment.text
                    while let sentence = Self.sentence(from: &pending) {
                        sentences.continuation.yield(VoiceFragment(text: sentence, agentID: fragment.agentID))
                    }
                }
                if !pending.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    sentences.continuation.yield(VoiceFragment(text: pending, agentID: authorID))
                }
                sentences.continuation.finish()
                try await self.speechTask?.value
                guard self.turn == current, !self.closed else { return }
                self.transcript = ""
                self.phase = .listening
            } catch {
                sentences.continuation.finish()
                guard self.turn == current, !self.closed, !Task.isCancelled else { return }
                self.speechTask?.cancel()
                self.output.stop()
                self.agentID = nil
                self.fail(error)
                await self.stop()
            }
        }
    }

    private func receive(_ event: VoiceEvent) {
        if muted {
            if case .transcript(let result) = event { consumedThrough = max(consumedThrough, result.end) }
            return
        }
        switch event {
        case .level(let value):
            level = value
            levels.removeFirst()
            levels.append(value)
            if value > 0.09 {
                audible = true
                soundAt = Date()
                if !segments.isEmpty, !finalizing { endpoint?.cancel() }
            } else if audible {
                audible = false
                if !segments.isEmpty { scheduleEnd() }
            }
            if value > 0.09, activity {
                if speechAt == nil { speechAt = Date() }
                if Date() > suppressUntil, Date().timeIntervalSince(speechAt ?? Date()) > 0.12 {
                    interrupt()
                }
            } else {
                speechAt = nil
            }
        case .activity(let detected):
            activity = detected
            if !detected, !audible, !segments.isEmpty { scheduleEnd() }
        case .transcript(let result):
            guard result.end > consumedThrough + 0.01 else { return }
            let text = result.text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !text.isEmpty else { return }
            if phase == .thinking || phase == .speaking { interrupt() }
            for segment in segments.values where segment.start < result.end && result.start < segment.end {
                segments.removeValue(forKey: segment.start)
            }
            segments[result.start] = .init(text: text, start: result.start, end: result.end, final: result.final)
            transcript = segments.keys.sorted().compactMap { segments[$0]?.text }.joined(separator: " ")
            reply.preview(transcript)
            if !audible, !finalizing { scheduleEnd() }
        }
    }

    private func scheduleEnd() {
        guard !finalizing else { return }
        endpoint?.cancel()
        let current = session
        endpoint = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(950))
            guard !Task.isCancelled, let self else { return }
            guard self.phase == .listening else { return }
            if self.segments.values.allSatisfy(\.final) { self.submit(); return }
            self.finalizing = true
            do {
                try await self.input.finalize()
                await Task.yield()
                guard self.session == current else { return }
                self.finalizing = false
                guard !Task.isCancelled, !self.closed, self.phase == .listening else { return }
                if !self.audible, Date().timeIntervalSince(self.soundAt) >= 0.95, self.segments.values.allSatisfy(\.final) { self.submit() }
                else { self.scheduleEnd() }
            } catch {
                guard self.session == current else { return }
                self.finalizing = false
                guard !Task.isCancelled, !self.closed else { return }
                self.fail(error)
                await self.stop()
            }
        }
    }

    private func fail(_ error: Error) {
        turn = UUID()
        endpoint?.cancel()
        replyTask?.cancel()
        speechTask?.cancel()
        output.stop()
        phase = .failed
        agentID = nil
        issue = error.localizedDescription
    }

    static func sentence(from text: inout String) -> String? {
        guard let boundary = text.firstIndex(where: { ".!?\n".contains($0) }) else { return nil }
        let end = text.index(after: boundary)
        let sentence = String(text[..<end]).trimmingCharacters(in: .whitespacesAndNewlines)
        text.removeSubrange(..<end)
        return sentence.isEmpty ? Self.sentence(from: &text) : sentence
    }
}
