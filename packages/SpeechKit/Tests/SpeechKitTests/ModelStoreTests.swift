import Foundation
import Testing
@testable import SpeechKit

/// The download archive is a plain ustar tarball; these pin extraction and the
/// checksum gate with a hand-built archive.
struct ModelStoreTests {
    /// Minimal ustar writer: one directory and two files, 512-byte blocks.
    private func makeTar() -> Data {
        var tar = Data()

        func octal(_ value: Int, width: Int) -> [UInt8] {
            var digits = String(value, radix: 8)
            digits = String(repeating: "0", count: width - digits.count) + digits
            return Array(digits.utf8) + [0]
        }

        func entry(name: String, type: UInt8, content: Data) {
            var header = [UInt8](repeating: 0, count: 512)
            let nameBytes = Array(name.utf8)
            header.replaceSubrange(0..<nameBytes.count, with: nameBytes)
            header.replaceSubrange(100..<108, with: octal(0o644, width: 7))
            header.replaceSubrange(108..<116, with: octal(0, width: 7))
            header.replaceSubrange(116..<124, with: octal(0, width: 7))
            header.replaceSubrange(124..<136, with: octal(content.count, width: 11))
            header.replaceSubrange(136..<148, with: octal(0, width: 11))
            header[156] = type
            let magic = Array("ustar\u{0}00".utf8)
            header.replaceSubrange(257..<257 + magic.count, with: magic)
            header.replaceSubrange(148..<156, with: [UInt8](repeating: UInt8(ascii: " "), count: 8))
            let checksum = header.reduce(0) { $0 + Int($1) }
            var check = octal(checksum, width: 6)
            check += [UInt8(ascii: " ")]
            header.replaceSubrange(148..<156, with: check)

            tar.append(Data(header))
            if type == UInt8(ascii: "0") {
                tar.append(content)
                let padding = (512 - content.count % 512) % 512
                tar.append(Data(repeating: 0, count: padding))
            }
        }

        entry(name: "glide-0.4b/", type: UInt8(ascii: "5"), content: Data())
        entry(name: "glide-0.4b/model.bin", type: UInt8(ascii: "0"), content: Data("hello".utf8))
        entry(name: "glide-0.4b/Data/weights.bin", type: UInt8(ascii: "0"), content: Data((0..<300).map { UInt8($0 % 251) }))
        tar.append(Data(repeating: 0, count: 1024))
        return tar
    }

    @Test func extractsArchiveEntries() throws {
        let tar = makeTar()
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("gate-test-\(UUID().uuidString).tar")
        try tar.write(to: url)
        defer { try? FileManager.default.removeItem(at: url) }

        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("gate-test-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: dir) }

        try ModelStore.extractTar(url, into: dir)
        let fm = FileManager.default
        #expect(fm.fileExists(atPath: dir.appendingPathComponent("glide-0.4b").path))
        #expect(try Data(contentsOf: dir.appendingPathComponent("glide-0.4b/model.bin")) == Data("hello".utf8))
        let weights = try Data(contentsOf: dir.appendingPathComponent("glide-0.4b/Data/weights.bin"))
        #expect(weights.count == 300)
        #expect(weights[299] == UInt8(299 % 251))
    }

    @Test func truncatedArchiveThrows() throws {
        // Cut 100 bytes into the last file's content, past its declared size.
        let tar = makeTar().prefix(512 * 4 + 100)
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("gate-test-\(UUID().uuidString).tar")
        try Data(tar).write(to: url)
        defer { try? FileManager.default.removeItem(at: url) }

        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("gate-test-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: dir) }

        #expect(throws: ModelError.archive) { try ModelStore.extractTar(url, into: dir) }
    }

    @Test func sha256MatchesKnownDigest() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("gate-test-\(UUID().uuidString)")
        try Data("hello".utf8).write(to: url)
        defer { try? FileManager.default.removeItem(at: url) }
        #expect(try ModelStore.sha256(url) == "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
    }
}
