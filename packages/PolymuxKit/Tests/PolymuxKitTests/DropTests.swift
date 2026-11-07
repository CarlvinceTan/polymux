#if os(macOS)
import AppKit
import CoreTransferable
import Testing
import UniformTypeIdentifiers
@testable import PolymuxKit

@MainActor
struct DropTests {
    @Test func dwellOpensOnlyWhereADragRests() async {
        let dwell = DropDwell(wait: .milliseconds(40))
        var opened: [String] = []
        // Moving within one row keeps restarting the wait, so a slow pass opens nothing.
        for step in 0..<6 {
            dwell.hold("passing", at: CGPoint(x: 10, y: CGFloat(step) * 10), open: { opened.append($0) })
            try? await Task.sleep(for: .milliseconds(15))
        }
        #expect(opened.isEmpty)
        dwell.hold("resting", at: CGPoint(x: 10, y: 200), open: { opened.append($0) })
        dwell.hold("resting", at: CGPoint(x: 11, y: 201), open: { opened.append($0) })
        try? await Task.sleep(for: .milliseconds(100))
        #expect(opened == ["resting"])
        dwell.hold("left", at: CGPoint(x: 10, y: 300), open: { opened.append($0) })
        dwell.hold(nil, at: .zero, open: { opened.append($0) })
        try? await Task.sleep(for: .milliseconds(100))
        #expect(opened == ["resting"])
    }

    @Test func chatsTargetWholeSectionsButNotBlankSpace() {
        let frames: [SidebarArea: CGRect] = [
            .pins: CGRect(x: 0, y: 0, width: 300, height: 120),
            .section("projects"): CGRect(x: 0, y: 130, width: 300, height: 200),
            .section("collapsed"): CGRect(x: 0, y: 340, width: 300, height: 30),
            .unassigned: CGRect(x: 0, y: 380, width: 300, height: 44)
        ]
        let sections = ["projects", "collapsed", SidebarLayout.unassignedID]
        func target(_ y: CGFloat) -> SidebarDestination? {
            DropTarget.sidebar(at: CGPoint(x: 150, y: y), item: .chat("chat"), frames: frames,
                               sections: sections, pins: [])
        }
        #expect(target(60) == .pin(before: nil))
        #expect(target(140) == .chats(.section("projects")))
        #expect(target(300) == .chats(.section("projects")))
        #expect(target(355) == .chats(.section("collapsed")))
        #expect(target(400) == .chats(.unassigned))
        #expect(target(335) == nil)
        #expect(target(600) == nil)
        #expect(DropTarget.sidebar(at: .zero, item: .chat("chat"), frames: [:], sections: [], pins: []) == nil)
    }

    @Test func sectionsUseNearestSectionAndDirectionalInsertion() {
        let frames: [SidebarArea: CGRect] = [
            .pins: CGRect(x: 0, y: 0, width: 300, height: 120),
            .section("projects"): CGRect(x: 0, y: 130, width: 300, height: 200),
            .section("collapsed"): CGRect(x: 0, y: 340, width: 300, height: 30),
            .unassigned: CGRect(x: 0, y: 380, width: 300, height: 44)
        ]
        let sections = ["projects", "collapsed", SidebarLayout.unassignedID]
        func target(_ source: String, _ y: CGFloat) -> SidebarDestination? {
            DropTarget.sidebar(at: CGPoint(x: 150, y: y), item: .section(source), frames: frames,
                               sections: sections, pins: [])
        }
        #expect(target("projects", 355) == .section("collapsed", after: true))
        #expect(target("collapsed", 230) == .section("projects", after: false))
        #expect(target("projects", 600) == .section(SidebarLayout.unassignedID, after: true))
        #expect(target("collapsed", 60) == .section("projects", after: false))
        #expect(target("projects", 230) == nil)
    }

    @Test func pinsInsertAcrossRowsAndKeepTheirSlotAtTheSource() {
        let frames: [SidebarArea: CGRect] = [
            .pins: CGRect(x: 0, y: 0, width: 300, height: 210),
            .pin("one"): CGRect(x: 0, y: 0, width: 95, height: 99),
            .pin("two"): CGRect(x: 99, y: 0, width: 95, height: 99),
            .pin("three"): CGRect(x: 198, y: 0, width: 95, height: 99),
            .pin("four"): CGRect(x: 0, y: 111, width: 95, height: 99)
        ]
        let pins = ["one", "two", "three", "four"]
        func target(_ x: CGFloat, _ y: CGFloat) -> SidebarDestination? {
            DropTarget.sidebar(at: CGPoint(x: x, y: y), item: .chat("new"), frames: frames, sections: [], pins: pins)
        }
        #expect(target(20, 50) == .pin(before: "one"))
        #expect(target(100, 50) == .pin(before: "two"))
        #expect(target(295, 50) == .pin(before: "four"))
        #expect(target(20, 170) == .pin(before: "four"))
        #expect(target(200, 170) == .pin(before: nil))
        #expect(DropTarget.pins(pins, moving: "three", before: "one") == ["three", "one", "two", "four"])
        #expect(DropTarget.pins(pins, moving: "one", before: nil) == ["two", "three", "four", "one"])
        #expect(DropTarget.pins(pins, moving: "two", before: "two") == pins)
        #expect(DropTarget.pins(pins, moving: "new", before: "two") == ["one", "new", "two", "three", "four"])
    }

    @Test func sidebarTransferKeepsItsOwnType() async throws {
        let provider = SidebarItem.chat("chief").provider()
        #expect(provider.registeredTypeIdentifiers == [SidebarItem.contentType.identifier])
        #expect(!DroppedFile.types.contains { provider.hasItemConformingToTypeIdentifier($0) })
        let item: SidebarItem = try await withCheckedThrowingContinuation { continuation in
            _ = provider.loadTransferable(type: SidebarItem.self) { continuation.resume(with: $0) }
        }
        #expect(item == .chat("chief"))
    }

    @Test func sidebarItemsStaySeparateFromFileAndTextDrops() throws {
        for item in [SidebarItem.chat("same-id"), .section("same-id")] {
            let data = try JSONEncoder().encode(item)
            #expect(try JSONDecoder().decode(SidebarItem.self, from: data) == item)
        }
        #expect(throws: DecodingError.self) {
            try JSONDecoder().decode(SidebarItem.self, from: Data("\"https://example.invalid/image.png\"".utf8))
        }
        #expect(!SidebarItem.contentType.conforms(to: .text))
        #expect(!DroppedFile.types.compactMap { UTType($0) }.contains { SidebarItem.contentType.conforms(to: $0) })
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        let item = NSPasteboardItem()
        item.setData(try JSONEncoder().encode(SidebarItem.chat("same-id")),
                     forType: NSPasteboard.PasteboardType(SidebarItem.contentType.identifier))
        item.setData(png, forType: .png)
        #expect(board.writeObjects([item]))
        #expect(DroppedFile.read(board).isEmpty)
    }

    @Test func targetFollowsCurrentWindowAndRows() {
        let frames = ["aria": CGRect(x: 18, y: 292, width: 284, height: 60),
                      "chief": CGRect(x: 18, y: 352, width: 284, height: 60)]
        let window = CGRect(x: 500, y: 132, width: 1000, height: 800)
        #expect(DropTarget.chat(at: CGPoint(x: 650, y: 609), frames: frames, window: window) == "aria")
        #expect(DropTarget.chat(at: CGPoint(x: 650, y: 549), frames: frames, window: window) == "chief")
        #expect(DropTarget.chat(at: CGPoint(x: 505, y: 549), frames: frames, window: window) == nil)
        #expect(DropTarget.chat(at: CGPoint(x: 650, y: 440), frames: frames, window: window) == nil)
        let moved = window.offsetBy(dx: 100, dy: -50)
        #expect(DropTarget.chat(at: CGPoint(x: 750, y: 499), frames: frames, window: moved) == "chief")
        let reordered = ["aria": frames["chief"]!, "chief": frames["aria"]!]
        #expect(DropTarget.chat(at: CGPoint(x: 650, y: 549), frames: reordered, window: window) == "aria")
    }

    @Test func filesKeepNamesAndSeparateCopies() async throws {
        let source = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: source) }
        let first = source.appending(path: "one/Photo 雪.txt")
        let second = source.appending(path: "two/Photo 雪.txt")
        for (index, file) in [first, second].enumerated() {
            try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
            try Data("File \(index)".utf8).write(to: file)
        }
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        #expect(board.writeObjects([first as NSURL, second as NSURL]))
        let results = try await collect(board, count: 2)
        defer { remove(results) }
        try FileManager.default.removeItem(at: source)
        var contents: Set<Data> = []
        for result in results {
            guard case let .file(file) = result else { Issue.record("File failed to import"); continue }
            #expect(file.lastPathComponent == "Photo 雪.txt")
            contents.insert(try Data(contentsOf: file))
        }
        #expect(contents == [Data("File 0".utf8), Data("File 1".utf8)])
    }

    @Test func imageWinsOverItsWebAddress() async throws {
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        let item = NSPasteboardItem()
        item.setString("https://example.invalid/photo.png", forType: .URL)
        item.setString("https://example.invalid/photo.png", forType: .string)
        item.setData(png, forType: .png)
        #expect(board.writeObjects([item]))
        let results = try await collect(board)
        defer { remove(results) }
        guard case let .file(file) = results[0] else { Issue.record("Image failed to import"); return }
        #expect(file.pathExtension == "png")
        #expect(try Data(contentsOf: file) == png)
    }

    @Test func fileWinsOverItsImageRepresentation() async throws {
        let source = FileManager.default.temporaryDirectory.appending(path: "Original-\(UUID()).png")
        try png.write(to: source)
        defer { try? FileManager.default.removeItem(at: source) }
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        let item = NSPasteboardItem()
        item.setString(source.absoluteString, forType: .fileURL)
        item.setData(png, forType: .png)
        #expect(board.writeObjects([item]))
        let results = try await collect(board)
        defer { remove(results) }
        guard case let .file(file) = results[0] else { Issue.record("File failed to import"); return }
        #expect(file.lastPathComponent == source.lastPathComponent)
    }

    @Test func directoriesAndInvalidImagesFail() async throws {
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        let image = NSPasteboardItem()
        image.setData(Data("not an image".utf8), forType: .png)
        #expect(board.writeObjects([FileManager.default.temporaryDirectory as NSURL, image]))
        #expect(try await collect(board, count: 2) == [.failed, .failed])
    }

    @Test func plainLinksAndTextAreNotAttachments() {
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        for (text, type) in [("https://example.invalid", NSPasteboard.PasteboardType.URL),
                             ("Some text", .string)] {
            board.clearContents()
            board.setString(text, forType: type)
            #expect(DroppedFile.read(board).isEmpty)
        }
        let text = NSItemProvider(object: "Some text" as NSString)
        #expect(!DroppedFile.types.contains { text.hasItemConformingToTypeIdentifier($0) })
    }

    @Test func promisesAreReadAsReceivers() throws {
        let board = NSPasteboard.withUniqueName()
        defer { board.releaseGlobally() }
        let delegate = Promise(data: png)
        let provider = NSFilePromiseProvider(fileType: UTType.png.identifier, delegate: delegate)
        defer { withExtendedLifetime((delegate, provider)) {} }
        #expect(board.writeObjects([provider]))
        let objects = DroppedFile.read(board)
        #expect(objects.count == 1)
        let receiver = try #require(objects.first as? NSFilePromiseReceiver)
        #expect(receiver.fileTypes == [UTType.png.identifier])
    }

    @Test func promiseCompletionRunsOffMainAndReportsFailures() async throws {
        let file = FileManager.default.temporaryDirectory.appending(path: "Promise-\(UUID()).png")
        try png.write(to: file)
        defer { try? FileManager.default.removeItem(at: file) }
        var results: [DroppedFile] = []
        let reader = DroppedFile.reader { results.append($0) }
        await Task.detached {
            reader(file, nil)
            reader(file, CocoaError(.fileWriteUnknown))
            reader(FileManager.default.temporaryDirectory, nil)
        }.value
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        while results.count < 3 && .now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        #expect(results.filter { $0 == .file(file) }.count == 1)
        #expect(results.filter { $0 == .failed }.count == 2)
    }

    private func collect(_ board: NSPasteboard, count: Int = 1) async throws -> [DroppedFile] {
        var results: [DroppedFile] = []
        let folder = try await DroppedFile.prepare()
        // Only this test's files are removed, after checking the imported copies.
        defer { if results.allSatisfy({ $0 == .failed }) { try? FileManager.default.removeItem(at: folder) } }
        try #require(DroppedFile.receive(from: board, into: folder) { results.append($0) })
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        while results.count < count && .now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(results.count == count)
        return results
    }

    private func remove(_ results: [DroppedFile]) {
        for result in results {
            if case let .file(url) = result {
                try? FileManager.default.removeItem(at: url.deletingLastPathComponent().deletingLastPathComponent())
            }
        }
    }

    private var png: Data {
        Data(base64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a8ioAAAAASUVORK5CYII=")!
    }
}

private final class Promise: NSObject, NSFilePromiseProviderDelegate {
    let data: Data?

    init(data: Data?) { self.data = data }

    @MainActor
    func filePromiseProvider(_ filePromiseProvider: NSFilePromiseProvider, fileNameForType fileType: String) -> String {
        "Promised.png"
    }

    func filePromiseProvider(_ filePromiseProvider: NSFilePromiseProvider, writePromiseTo url: URL,
                             completionHandler: @escaping ((any Error)?) -> Void) {
        do {
            guard let data else { throw CocoaError(.fileWriteUnknown) }
            try data.write(to: url)
            completionHandler(nil)
        } catch { completionHandler(error) }
    }

    @MainActor
    func operationQueue(for filePromiseProvider: NSFilePromiseProvider) -> OperationQueue { OperationQueue() }
}
#endif
