import Foundation

public enum DictationLanguage: String, CaseIterable, Identifiable, Sendable {
    case english, multilingual

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .english: "English"
        case .multilingual: "Multilingual"
        }
    }
}

/// The post-processing a dictation gets before it reaches the composer.
public struct DictationOptions: Equatable, Sendable {
    public var language: DictationLanguage
    public var fillerWords: Bool
    public var smartSpacing: Bool
    public var spokenSymbols: Bool
    public var spokenFormatting: Bool
    public var outputFixes: Bool
    public var polish: Bool

    public init(language: DictationLanguage = .multilingual, fillerWords: Bool = true,
                smartSpacing: Bool = true, spokenSymbols: Bool = true,
                spokenFormatting: Bool = false, outputFixes: Bool = true, polish: Bool = true) {
        self.language = language
        self.fillerWords = fillerWords
        self.smartSpacing = smartSpacing
        self.spokenSymbols = spokenSymbols
        self.spokenFormatting = spokenFormatting
        self.outputFixes = outputFixes
        self.polish = polish
    }
}

/// What this device can run: phones always allow the multilingual path, while a Mac
/// falls back to English when it lacks the memory for the larger language assets.
public enum DictationDevice {
    public static var supportsMultilingual: Bool {
        #if os(iOS)
        true
        #else
        #if arch(arm64)
        ProcessInfo.processInfo.physicalMemory >= 16 * 1024 * 1024 * 1024
        #else
        false
        #endif
        #endif
    }

    /// The language to actually recognise, after the device policy.
    public static func language(_ preferred: DictationLanguage) -> DictationLanguage {
        preferred == .multilingual && !supportsMultilingual ? .english : preferred
    }

    /// English prefers the system's own variant (en-AU, en-GB, …) when it is English.
    public static func locale(for language: DictationLanguage) -> Locale {
        let current = Locale.current
        if language == .multilingual { return current }
        return current.identifier.hasPrefix("en") ? current : Locale(identifier: "en-US")
    }

    /// The language recognition will actually listen for, named for the current locale.
    public static func languageName(for language: DictationLanguage) -> String {
        let locale = locale(for: language)
        let code = locale.identifier.split(whereSeparator: { $0 == "_" || $0 == "-" }).first.map(String.init)
            ?? locale.identifier
        return Locale.current.localizedString(forLanguageCode: code) ?? code.capitalized
    }
}
