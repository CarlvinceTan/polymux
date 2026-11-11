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

    /// How many dictation models can run together for multilingual input.
    public static var maxLocales: Int {
        #if os(iOS)
        2
        #else
        ProcessInfo.processInfo.physicalMemory >= 32 * 1024 * 1024 * 1024 ? 3 : 2
        #endif
    }

    /// The language to actually recognise, after the device policy.
    public static func language(_ preferred: DictationLanguage) -> DictationLanguage {
        preferred == .multilingual && !supportsMultilingual ? .english : preferred
    }

    /// The locales dictation should run. English is always first; multilingual adds
    /// the non-English languages configured on this device, up to `maxLocales`.
    public static func locales(for language: DictationLanguage) -> [Locale] {
        let english = englishLocale
        guard language == .multilingual else { return [english] }
        var out = [english]
        var codes: Set<String> = ["en"]
        for identifier in Locale.preferredLanguages {
            let locale = Locale(identifier: identifier)
            let code = locale.language.languageCode?.identifier ?? identifier
            guard !codes.contains(code) else { continue }
            codes.insert(code)
            out.append(locale)
            if out.count == maxLocales { break }
        }
        return out
    }

    /// English prefers the system's own variant (en-AU, en-GB, …) when it is English.
    public static func locale(for language: DictationLanguage) -> Locale {
        if language == .english { return englishLocale }
        return Locale.current
    }

    /// The languages recognition will listen for, named for the current locale.
    public static func languageNames(for language: DictationLanguage) -> String {
        var names: [String] = []
        for locale in locales(for: language) {
            let code = locale.language.languageCode?.identifier ?? locale.identifier
            let name = Locale.current.localizedString(forLanguageCode: code) ?? code.capitalized
            guard !names.contains(name) else { continue }
            names.append(name)
        }
        return names.joined(separator: ", ")
    }

    private static var englishLocale: Locale {
        let current = Locale.current
        return current.identifier.hasPrefix("en") ? current : Locale(identifier: "en-US")
    }
}
