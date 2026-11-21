import Testing
@testable import SpeechKit

@Test func bestFinalUsesConfidenceThenLength() {
    let short = DictationFinal(text: "short", confidence: 0.8)
    let long = DictationFinal(text: "a longer transcript", confidence: 0.8)
    let unsure = DictationFinal(text: "unsure", confidence: 0.3)
    #expect(DictationChoice.best(["a": short, "b": long, "c": unsure]) == long)
    #expect(DictationChoice.best(["a": unsure]) == unsure)
    #expect(DictationChoice.best([:]) == nil)
}

@Test func englishUsesOneLocaleAndMultilingualStartsWithEnglish() {
    let english = DictationDevice.locales(for: .english)
    #expect(english.count == 1)
    #expect(english.first?.language.languageCode?.identifier == "en")

    let multiple = DictationDevice.locales(for: .multilingual)
    #expect(multiple.first?.language.languageCode?.identifier == "en")
    #expect(multiple.count <= DictationDevice.maxLocales)
    let codes = multiple.compactMap { $0.language.languageCode?.identifier }
    #expect(Set(codes).count == codes.count)
}

@Test func englishIsAlwaysKept() {
    #expect(DictationDevice.language(.english) == .english)
    if DictationDevice.supportsMultilingual {
        #expect(DictationDevice.language(.multilingual) == .multilingual)
    } else {
        #expect(DictationDevice.language(.multilingual) == .english)
    }
}

@Test func coordinatorPicksTheMostConfidentFinal() async {
    let stream = AsyncThrowingStream<VoiceEvent, Error>.makeStream()
    let coordinator = DictationCoordinator(continuation: stream.continuation, locales: ["en", "es"])
    let collected = Task {
        var events: [VoiceEvent] = []
        do {
            for try await event in stream.stream { events.append(event) }
        } catch {}
        return events
    }
    await coordinator.accept(text: "Hello", start: 0, end: 1, final: false, confidence: 0, locale: "en")
    await coordinator.accept(text: "Hola", start: 0, end: 1, final: false, confidence: 0, locale: "es")
    await coordinator.accept(text: "Hello there", start: 0, end: 1.2, final: true, confidence: 0.9, locale: "en")
    await coordinator.accept(text: "Hola", start: 0, end: 1.2, final: true, confidence: 0.3, locale: "es")
    await coordinator.finish()
    let events = await collected.value
    let transcripts = events.compactMap { event -> VoiceTranscript? in
        guard case let .transcript(value) = event else { return nil }
        return value
    }
    #expect(transcripts.contains { $0.final && $0.text == "Hello there" })
    #expect(!transcripts.contains { $0.final && $0.text == "Hola" })
}
