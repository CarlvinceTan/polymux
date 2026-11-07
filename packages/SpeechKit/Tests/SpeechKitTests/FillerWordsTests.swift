import Testing
@testable import SpeechKit

@Test func fillersLeaveWithTheirCommasAndSpaces() {
    #expect(FillerWords.apply("So, um, we should go.") == "So, we should go.")
    #expect(FillerWords.apply("I think uh we should go.") == "I think we should go.")
    #expect(FillerWords.apply("I think, um.") == "I think.")
    #expect(FillerWords.apply("Well umm... maybe") == "Well maybe")
    #expect(FillerWords.apply("It was, er, erm, fine") == "It was, fine")
    #expect(FillerWords.apply("Done, uh") == "Done")
    #expect(FillerWords.apply("(um, aside)") == "(aside)")
}

@Test func aFillerThatStartedASentenceHandsOnItsCapital() {
    #expect(FillerWords.apply("Um, so I think") == "So I think")
    #expect(FillerWords.apply("Hello. Uh, we left. Ah. Then") == "Hello. We left. Then")
    #expect(FillerWords.apply("Uh, um, okay") == "Okay")
}

@Test func lineBreaksAreKept() {
    #expect(FillerWords.apply("First line.\nUm, second") == "First line.\nSecond")
    #expect(FillerWords.apply("one um\ntwo") == "one\ntwo")
}

@Test func otherWordsAreLeftAlone() {
    for text in [
        "The umbrella is ahead of the error.",
        "Uhura said uh-huh.",
        "Take him to the ER.",
        "UM is a university.",
        "drum, Ahab, ermine",
        "Plain text, nothing to do.",
        "",
    ] {
        #expect(FillerWords.apply(text) == text)
    }
}
