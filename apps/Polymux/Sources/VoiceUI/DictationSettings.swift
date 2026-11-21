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

    /// Reviews dictation for misheard words and adds them to the dictionary by itself.
    public var autoReview: Bool {
        didSet { defaults.set(autoReview, forKey: Key.autoReview) }
    }

    /// Empty rows are kept while the user types; the vocabulary cleans them when used.
    public var vocabulary: [VocabularyEntry] {
        didSet {
            guard let data = try? JSONEncoder().encode(vocabulary) else { return }
            defaults.set(data, forKey: Key.vocabulary)
        }
    }

    /// Misheard words the model found, waiting for the user to add or dismiss them.
    public private(set) var suggestions: [VocabSuggestion] {
        didSet {
            guard let data = try? JSONEncoder().encode(suggestions) else { return }
            defaults.set(data, forKey: Key.suggestions)
        }
    }

    @ObservationIgnored private var dismissed: Set<String> {
        didSet { defaults.set(Array(dismissed), forKey: Key.dismissed) }
    }

    static let maxSuggestions = 12

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
        autoReview = defaults.object(forKey: Key.autoReview) as? Bool ?? false
        suggestions = defaults.data(forKey: Key.suggestions)
            .flatMap { try? JSONDecoder().decode([VocabSuggestion].self, from: $0) } ?? []
        dismissed = Set(defaults.stringArray(forKey: Key.dismissed) ?? [])
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

    /// Queues new suggestions, or adds them straight away when review is automatic, skipping ones already pending, dismissed or covered by the dictionary.
    public func suggest(_ found: [VocabSuggestion]) {
        for item in found where suggestions.count < Self.maxSuggestions {
            let covered = vocabulary.contains {
                $0.term.lowercased() == item.term.lowercased()
                    && $0.soundsLike.contains { $0.lowercased() == item.heard.lowercased() }
            }
            guard !covered, !dismissed.contains(item.id), !suggestions.contains(where: { $0.id == item.id }) else { continue }
            if autoReview { accept(item) } else { suggestions.append(item) }
        }
    }

    public func accept(_ item: VocabSuggestion) {
        suggestions.removeAll { $0.id == item.id }
        if let index = vocabulary.firstIndex(where: { $0.term.lowercased() == item.term.lowercased() }) {
            vocabulary[index].soundsLike.append(item.heard)
            vocabulary = Vocabulary.clean(vocabulary)
        } else if canAddEntry {
            vocabulary.append(VocabularyEntry(term: item.term, soundsLike: [item.heard]))
        }
    }

    public func dismiss(_ item: VocabSuggestion) {
        suggestions.removeAll { $0.id == item.id }
        dismissed.insert(item.id)
    }

    private enum Key {
        static let suggestions = "dictation.suggestions"
        static let dismissed = "dictation.dismissed"
        static let language = "dictation.language"
        static let fillerWords = "dictation.fillerWords"
        static let smartSpacing = "dictation.smartSpacing"
        static let spokenSymbols = "dictation.spokenSymbols"
        static let spokenFormatting = "dictation.spokenFormatting"
        static let outputFixes = "dictation.outputFixes"
        static let polish = "dictation.polish"
        static let autoReview = "dictation.autoReview"
        static let vocabulary = "dictation.vocabulary"
    }
}
