import Foundation
import FoundationModels

/// Fixes punctuation and obvious mishears in dictated text on-device; nil when unavailable.
enum DictationPolish {
    static func run(_ text: String) async -> String? {
        let model = SystemLanguageModel.default
        guard case .available = model.availability, text.count > 3 else { return nil }
        let session = LanguageModelSession(model: model, instructions: """
            You clean up speech-to-text. Fix punctuation, capitalisation and clearly misheard words. \
            Keep the speaker's wording and meaning. Reply with the corrected text only.
            """)
        do {
            let reply = try await session.respond(to: text, options: GenerationOptions(temperature: 0)).content
                .trimmingCharacters(in: .whitespacesAndNewlines)
            // Reject answers that drift far from the dictation.
            guard !reply.isEmpty, reply.count < text.count * 2 else { return nil }
            return reply
        } catch { return nil }
    }
}
