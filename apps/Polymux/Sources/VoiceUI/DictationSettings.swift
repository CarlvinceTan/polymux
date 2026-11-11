import Foundation
import Observation
import SpeechKit

/// Device-local dictation settings: each Mac or phone picks its own recognition
/// language and dictionary, since the hardware under it differs.
@MainActor @Observable
public final class DictationSettings {
    public static let shared = DictationSettings()

    public var language: DictationLanguage {
        didSet { defaults.set(language.rawValue, forKey: Key.language) }
    }

    public var fillerWords: Bool {
        didSet { defaults.set(fillerWords, forKey: Key.fillerWords) }
    }

    public var smartSpacing: Bool {
        didSet { defaults.set(smartSpacing, forKey: Key.smartSpacing) }
    }

    public var spokenSymbols: Bool {
        didSet { defaults.set(spokenSymbols, forKey: Key.spokenSymbols) }
    }

    public var spokenFormatting: Bool {
        didSet { defaults.set(spokenFormatting, forKey: Key.spokenFormatting) }
    }

    public var outputFixes: Bool {
        didSet { defaults.set(outputFixes, forKey: Key.outputFixes) }
    }

    public var polish: Bool {
        didSet { defaults.set(polish, forKey: Key.polish) }
    }

    /// Empty rows are kept while the user types; the vocabulary cleans them when used.
    public var vocabulary: [VocabularyEntry] {
        didSet {
            guard let data = try? JSONEncoder().encode(vocabulary) else { return }
            defaults.set(data, forKey: Key.vocabulary)
        }
    }

    @ObservationIgnored private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        language = DictationLanguage(rawValue: defaults.string(forKey: Key.language) ?? "") ?? .multilingual
        fillerWords = defaults.object(forKey: Key.fillerWords) as? Bool ?? true
        smartSpacing = defaults.object(forKey: Key.smartSpacing) as? Bool ?? true
        spokenSymbols = defaults.object(forKey: Key.spokenSymbols) as? Bool ?? true
        spokenFormatting = defaults.object(forKey: Key.spokenFormatting) as? Bool ?? false
        outputFixes = defaults.object(forKey: Key.outputFixes) as? Bool ?? true
        polish = defaults.object(forKey: Key.polish) as? Bool ?? DictationPolish.isAvailable
        if let data = defaults.data(forKey: Key.vocabulary),
           let saved = try? JSONDecoder().decode([VocabularyEntry].self, from: data) {
            vocabulary = Vocabulary.clean(saved)
        } else {
            vocabulary = []
        }
    }

    public var deviceLanguage: DictationLanguage { DictationDevice.language(language) }

    public var fallsBack: Bool { language == .multilingual && deviceLanguage == .english }

    public var options: DictationOptions {
        DictationOptions(language: deviceLanguage, fillerWords: fillerWords, smartSpacing: smartSpacing,
                         spokenSymbols: spokenSymbols, spokenFormatting: spokenFormatting,
                         outputFixes: outputFixes, polish: polish)
    }

    public var canAddEntry: Bool { vocabulary.count < Vocabulary.maxEntries }

    public func addEntry() {
        guard canAddEntry else { return }
        vocabulary.append(VocabularyEntry(term: "", soundsLike: []))
    }

    public func removeEntry(_ id: UUID) {
        vocabulary.removeAll { $0.id == id }
    }

    private enum Key {
        static let language = "dictation.language"
        static let fillerWords = "dictation.fillerWords"
        static let smartSpacing = "dictation.smartSpacing"
        static let spokenSymbols = "dictation.spokenSymbols"
        static let spokenFormatting = "dictation.spokenFormatting"
        static let outputFixes = "dictation.outputFixes"
        static let polish = "dictation.polish"
        static let vocabulary = "dictation.vocabulary"
    }
}
