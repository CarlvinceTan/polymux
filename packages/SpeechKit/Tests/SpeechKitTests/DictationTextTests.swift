import Testing
@testable import SpeechKit

@Test func fillersAreOptional() {
    let options = DictationOptions(fillerWords: true)
    #expect(DictationText.clean("So, um, we ship.", options: options) == "So, we ship.")
    #expect(DictationText.clean("So, um, we ship.", options: DictationOptions(fillerWords: false)) == "So, um, we ship.")
}

@Test func spokenSymbolsAreOnByDefault() {
    let options = DictationOptions(fillerWords: false)
    #expect(DictationText.clean("Use at sign here", options: options) == "Use @ here")
    #expect(DictationText.clean("Use hashtag here", options: options) == "Use # here")
    #expect(DictationText.clean("Hello comma world period", options: options) == "Hello comma world period")
}

@Test func spokenFormattingConvertsPunctuationWhenEnabled() {
    let options = DictationOptions(fillerWords: false, spokenFormatting: true, outputFixes: true)
    #expect(DictationText.clean("Hello comma world period", options: options) == "Hello, world.")
    #expect(DictationText.clean("Ready question mark New line Next", options: options) == "Ready?\nNext")
    #expect(DictationText.clean("The period was long.", options: options) == "The period was long.")
    #expect(DictationText.clean("Hello period Next.", options: options) == "Hello. Next.")
}

@Test func outputFixesCapitaliseAndTidySpacing() {
    let options = DictationOptions(fillerWords: false, spokenFormatting: false, outputFixes: true)
    #expect(DictationText.clean("hello , world . i think so .", options: options) == "Hello, world. I think so.")
    #expect(DictationText.clean("one  two\t three", options: options) == "One two three")
    #expect(DictationText.clean("and then we left", options: options, capitalizeStart: false) == "and then we left")
    #expect(DictationText.clean("ß tester", options: options) == "SS tester")
}

@Test func smartSpacingAvoidsJoinedWordsAndDoubleSpaces() {
    #expect(DictationText.spaced("world", before: "o", after: nil) == " world")
    #expect(DictationText.spaced("world", before: nil, after: "x") == "world ")
    #expect(DictationText.spaced("world", before: "o", after: "x") == " world ")
    #expect(DictationText.spaced("world", before: " ", after: nil) == "world")
    #expect(DictationText.spaced(",", before: "o", after: nil) == ",")
    #expect(DictationText.spaced("world", before: "o", after: ".") == " world")
    #expect(DictationText.spaced("world", before: "o", after: "!") == " world")
}
