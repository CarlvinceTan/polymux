import Foundation
import Observation
import PolymuxKit
import SpeechKit

/// One provider, model and voice a live session can run on.
struct LiveAttempt {
    let options: LiveOptions
    let makeProvider: () throws -> any LiveProvider
    let key: () throws -> String
}

@MainActor
@Observable
public final class LiveStore: VoiceSession {
    public private(set) var phase = VoicePhase.preparing
    public private(set) var transcript = ""
    public private(set) var response = ""
    public private(set) var level: Float = 0
    public private(set) var issue: String?
    public private(set) var muted = false
    public private(set) var silenced = false
    public private(set) var usage: LiveUsage?
    public private(set) var connected: Date?
    public var working: Int { jobs.count }
    private let attempts: [LiveAttempt]
    private let reply: any VoiceReply
    private let sound: any LiveSound
    /// The attempt in use; a failure moves on to the next.
    private var index = 0
    private var switching = false
    private var provider: (any LiveProvider)?
    private var startup: Task<Void, Never>?
    private var shutdown: Task<Void, Never>?
    private var reader: Task<Void, Never>?
    private var microphone: Task<Void, Never>?
    private var jobs: [String: Task<Void, Never>] = [:]
    private var cancellation: Task<Void, Never>?
    private var seen: Set<String> = []
    private var closed = true
    private var token = UUID()
    private var freshResponse = true
    private var freshTranscript = true
    private var idle = true

    init(attempts: [LiveAttempt], reply: any VoiceReply, sound: (any LiveSound)? = nil) {
        self.attempts = attempts
        self.reply = reply
        self.sound = sound ?? NativeSound()
        self.sound.drained = { [weak self] in
            guard let self, !self.closed else { return }
            self.phase = self.idle ? .listening : .thinking
        }
        self.sound.onInterruption { [weak self] began in
            guard let self, !self.closed, self.phase != .failed else { return }
            self.phase = began ? .interrupted : (self.idle ? .listening : .thinking)
        }
    }

    convenience init(options: LiveOptions, reply: any VoiceReply, sound: (any LiveSound)? = nil,
                     makeProvider: @escaping () throws -> any LiveProvider, key: @escaping () throws -> String) {
        self.init(attempts: [LiveAttempt(options: options, makeProvider: makeProvider, key: key)], reply: reply, sound: sound)
    }

    public func start() async {
        await shutdown?.value
        guard !Task.isCancelled else { return }
        if let startup { await startup.value; return }
        guard closed else { return }
        if !switching { index = 0 }
        closed = false
        issue = nil
        muted = false
        silenced = false
        transcript = ""
        response = ""
        usage = nil
        if !switching { connected = nil }
        freshResponse = true
        freshTranscript = true
        idle = true
        seen = []
        phase = .preparing
        let current = UUID()
        token = current
        let task = Task { [weak self] in
            guard let self else { return }
            await self.prepare(current)
        }
        startup = task
        await withTaskCancellationHandler {
            await task.value
        } onCancel: { task.cancel() }
        if token == current { startup = nil }
    }

    private func prepare(_ current: UUID) async {
        do {
            let attempt = attempts[index]
            let provider = try attempt.makeProvider()
            self.provider = provider
            var config = attempt.options
            config.tools = [Self.agentTool]
            let events = try await provider.start(options: config, key: attempt.key())
            try Task.checkCancellation()
            guard token == current, !closed else { return }
            let frames = try await sound.start(rate: provider.inputRate)
            try Task.checkCancellation()
            guard token == current, !closed else { sound.stop(); return }
            sound.setMuted(muted)
            phase = .listening
            if connected == nil {
                connected = Date()
                sound.tone(.connect)
            }
            microphone = Task { [weak self] in
                do {
                    for try await event in frames {
                        guard let self, self.token == current, !self.closed, !Task.isCancelled else { return }
                        switch event {
                        case .frame(let pcm, let level):
                            guard !self.muted else { continue }
                            self.level = level
                            try await provider.sendAudio(pcm)
                        case .end: try await provider.endAudio()
                        }
                    }
                    if !Task.isCancelled { throw SpeechFailure.audio }
                } catch {
                    guard !Task.isCancelled, self?.token == current else { return }
                    await self?.fail(error)
                }
            }
            reader = Task { [weak self] in
                do {
                    for try await event in events {
                        guard let self, self.token == current, !self.closed, !Task.isCancelled else { return }
                        try self.receive(event, current: current)
                    }
                    if !Task.isCancelled { throw LiveFailure.ended }
                } catch {
                    guard !Task.isCancelled, self?.token == current else { return }
                    await self?.fail(error)
                }
            }
        } catch {
            guard token == current else { return }
            sound.stop()
            await provider?.stop()
            if !Task.isCancelled, index + 1 < attempts.count {
                index += 1
                await prepare(current)
                return
            }
            closed = true
            if !Task.isCancelled { phase = .failed; issue = error.voiceIssue }
        }
    }

    public func stop() async {
        if let shutdown { await shutdown.value; return }
        guard !closed else { return }
        closed = true
        token = UUID()
        let preparing = startup
        startup = nil
        preparing?.cancel()
        reader?.cancel()
        microphone?.cancel()
        reader = nil
        microphone = nil
        let hadJobs = !jobs.isEmpty
        for job in jobs.values { job.cancel() }
        jobs = [:]
        let cancelling = cancellation
        cancellation = nil
        sound.stop()
        if connected != nil, !switching { sound.tone(.end) }
        level = 0
        let provider = self.provider
        self.provider = nil
        let task = Task { [reply] in
            await provider?.stop()
            await preparing?.value
            await cancelling?.value
            if hadJobs { await reply.stop() }
        }
        shutdown = task
        await task.value
        shutdown = nil
    }

    public func retry() async {
        await stop()
        guard !Task.isCancelled else { return }
        await start()
    }

    public func toggleSilence() {
        guard !closed else { return }
        silenced.toggle()
        sound.setSilenced(silenced)
    }

    public func toggleMute() {
        guard !closed else { return }
        muted.toggle()
        sound.setMuted(muted)
        level = 0
    }

    private func receive(_ event: LiveEvent, current: UUID) throws {
        switch event {
        case .transcript(let text):
            if freshTranscript { transcript = ""; freshTranscript = false }
            transcript += text
        case .response(let text):
            if freshResponse { response = ""; freshResponse = false }
            response += text
            idle = false
            if !sound.playing { phase = .thinking }
        case .audio(let chunk):
            if freshResponse { response = ""; freshResponse = false }
            idle = false
            try sound.play(chunk)
            phase = .speaking
        case .interrupted:
            sound.interrupt()
            freshResponse = true
            phase = .listening
            idle = true
        case .turnComplete(let idle):
            self.idle = idle
            freshResponse = idle
            freshTranscript = idle
            if !sound.playing { phase = idle ? .listening : .thinking }
        case .calls(let calls): for call in calls { delegate(call, current: current) }
        case .cancel(let ids): cancel(ids, current: current)
        case .usage(let usage): self.usage = usage
        }
    }

    private func delegate(_ call: LiveCall, current: UUID) {
        guard !seen.contains(call.id) else { return }
        guard seen.count < 256 else {
            Task { if token == current, !closed { await fail(LiveFailure.backlog) } }
            return
        }
        seen.insert(call.id)
        let args = try? JSONDecoder().decode(AgentArgs.self, from: call.arguments)
        let text = args?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let failure: String?
        if call.name != "ask_agent" || text.isEmpty || text.count > 16384 {
            failure = "Unsupported tool or invalid request."
        } else if !jobs.isEmpty || cancellation != nil {
            failure = "The agent is already working. Wait for its result before sending another task."
        } else { failure = nil }
        if let failure {
            Task { [weak self] in
                await self?.respond(LiveResult(call: call, text: failure, failed: true), current: current)
            }
            return
        }
        jobs[call.id] = Task { [weak self] in
            guard let self else { return }
            var result = ""
            var failed = false
            do {
                for try await fragment in self.reply.stream(text) {
                    try Task.checkCancellation()
                    result += fragment.text.prefix(max(0, 160000 - result.count))
                }
                try Task.checkCancellation()
            } catch {
                guard !Task.isCancelled else { return }
                result = "The consultation failed and its outcome is unconfirmed. Actions may already have occurred. Do not retry automatically; ask the user before retrying."
                failed = true
            }
            guard self.token == current, !self.closed, !Task.isCancelled else { return }
            self.jobs.removeValue(forKey: call.id)
            await self.respond(LiveResult(call: call, text: result, failed: failed), current: current)
        }
    }

    private func cancel(_ ids: [String], current: UUID) {
        let targets = ids.filter { jobs[$0] != nil }
        guard !targets.isEmpty, cancellation == nil else { return }
        for id in targets { jobs[id]?.cancel() }
        cancellation = Task { [weak self] in
            guard let self else { return }
            await self.reply.stop()
            guard self.token == current else { return }
            for id in targets { self.jobs.removeValue(forKey: id) }
            self.cancellation = nil
        }
    }

    private func respond(_ result: LiveResult, current: UUID) async {
        guard token == current, !closed, let provider else { return }
        do { try await provider.respond(result) }
        catch {
            guard token == current, !Task.isCancelled else { return }
            await fail(error)
        }
    }

    private func fail(_ error: Error) async {
        // A provider that drops mid-session hands over to the next one; the conversation starts fresh.
        if error is LiveFailure, index + 1 < attempts.count {
            switching = true
            await stop()
            index += 1
            // The caller is a task stop() cancelled, so the next session starts on a fresh one.
            await Task { await start() }.value
            switching = false
            return
        }
        phase = .failed
        issue = error.voiceIssue
        await stop()
    }

    private struct AgentArgs: Decodable { let text: String }

    static let agentTool = LiveTool(
        name: "ask_agent",
        description: "Ask the existing Polymux agent to perform a task or answer a question requiring tools or private context.",
        parameters: Data(#"{"type":"object","properties":{"text":{"type":"string","description":"The complete user request, including relevant conversational context."}},"required":["text"]}"#.utf8)
    )
}
