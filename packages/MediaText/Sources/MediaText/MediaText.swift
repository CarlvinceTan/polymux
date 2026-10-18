import AVFoundation
import Foundation
import Speech
import Vision

private struct Extraction: Encodable {
    let text: String
}

private enum ExtractionError: LocalizedError {
    case arguments
    case unsupported
    case speech
    case language

    var errorDescription: String? {
        switch self {
        case .arguments: "Usage: media-text <file path> <mime type>"
        case .unsupported: "Only images and audio support extracted text."
        case .speech: "On-device speech recognition is unavailable on this Mac."
        case .language: "The current language is unsupported for on-device speech recognition."
        }
    }
}

@main
private struct MediaText {
    static func main() async {
        do {
            guard CommandLine.arguments.count == 3 else { throw ExtractionError.arguments }
            let url = URL(fileURLWithPath: CommandLine.arguments[1])
            let mime = CommandLine.arguments[2].lowercased()
            let text: String
            if mime.hasPrefix("image/") {
                text = try image(url)
            } else if mime.hasPrefix("audio/") {
                text = try await audio(url)
            } else {
                throw ExtractionError.unsupported
            }
            let data = try JSONEncoder().encode(Extraction(text: text))
            FileHandle.standardOutput.write(data)
            FileHandle.standardOutput.write(Data("\n".utf8))
        } catch {
            FileHandle.standardError.write(Data((error.localizedDescription + "\n").utf8))
            exit(1)
        }
    }

    private static func image(_ url: URL) throws -> String {
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.usesLanguageCorrection = true
        request.automaticallyDetectsLanguage = true
        try VNImageRequestHandler(url: url).perform([request])
        let observations = (request.results ?? []).sorted {
            if abs($0.boundingBox.midY - $1.boundingBox.midY) < 0.015 {
                return $0.boundingBox.minX < $1.boundingBox.minX
            }
            return $0.boundingBox.midY > $1.boundingBox.midY
        }
        return observations.compactMap { $0.topCandidates(1).first?.string }.joined(separator: "\n")
    }

    private static func audio(_ url: URL) async throws -> String {
        guard SpeechTranscriber.isAvailable else { throw ExtractionError.speech }
        guard let locale = await SpeechTranscriber.supportedLocale(equivalentTo: .current) else {
            throw ExtractionError.language
        }
        let transcriber = SpeechTranscriber(locale: locale, preset: .transcription)
        let reserved = try await AssetInventory.reserve(locale: locale)
        do {
            if let installation = try await AssetInventory.assetInstallationRequest(supporting: [transcriber]) {
                try await installation.downloadAndInstall()
            }
            let file = try AVAudioFile(forReading: url)
            let analyzer = SpeechAnalyzer(modules: [transcriber])
            let results = Task {
                var segments: [Double: String] = [:]
                for try await result in transcriber.results {
                    segments[result.range.start.seconds] = String(result.text.characters)
                }
                return segments.keys.sorted().compactMap { segments[$0] }.joined(separator: " ")
            }
            do {
                try await analyzer.start(inputAudioFile: file, finishAfterFile: true)
                let text = try await results.value
                if reserved { await AssetInventory.release(reservedLocale: locale) }
                return text
            } catch {
                results.cancel()
                await analyzer.cancelAndFinishNow()
                throw error
            }
        } catch {
            if reserved { await AssetInventory.release(reservedLocale: locale) }
            throw error
        }
    }
}
