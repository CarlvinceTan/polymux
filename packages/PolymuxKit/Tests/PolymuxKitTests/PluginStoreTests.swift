import Foundation
import Testing
@testable import PolymuxKit

@Suite @MainActor struct PluginStoreTests {
    private func plugin(_ name: String, kind: String = PluginKind.scoped, surface: String? = nil,
                        state: String = PluginState.ready, chats: [String] = []) -> Plugin {
        Plugin(name: name, title: name, kind: kind, surface: surface ?? name, state: state, chats: chats)
    }

    private func store(_ list: PluginList) async throws -> PluginStore {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let cache = LocalCacheActor(try await CacheWork.open(folder.appendingPathComponent("cache.sqlite")))
        await cache.saveSnapshot("plugins", list)
        let value = PluginStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache,
                                surfaces: ["hub", "a", "b", "c"])
        await value.restore()
        return value
    }

    @Test func switcherStaysHiddenUntilAGlobalPluginCanShow() async throws {
        let none = try await store(PluginList(layout: PluginLayout(switcher: ["chats"])))
        #expect(none.tabs.isEmpty)
        let installing = try await store(PluginList(installed: [plugin("hub", kind: PluginKind.global, state: PluginState.installing)],
                                                    layout: PluginLayout(switcher: ["chats", "hub"])))
        #expect(installing.tabs.isEmpty)
        let unknown = try await store(PluginList(installed: [plugin("new", kind: PluginKind.global)],
                                                 layout: PluginLayout(switcher: ["chats", "new"])))
        #expect(unknown.tabs.isEmpty)
        let ready = try await store(PluginList(installed: [plugin("hub", kind: PluginKind.global)],
                                               layout: PluginLayout(switcher: ["hub", "chats"])))
        #expect(ready.tabs.map(\.id) == ["hub", "chats"])
        #expect(ready.tabs[1].plugin == nil)
    }

    @Test func buttonsKeepSharedOrderAndOnlyActivePlugins() async throws {
        let value = try await store(PluginList(
            installed: [plugin("a", chats: ["one", "group"]), plugin("b", chats: ["one"]), plugin("c", chats: ["group"])],
            layout: PluginLayout(buttons: [["a", "b"], ["c"]])))
        #expect(value.buttons(chat: "one").map { $0.map(\.name) } == [["a", "b"]])
        #expect(value.buttons(chat: "group").map { $0.map(\.name) } == [["a"], ["c"]])
        #expect(value.buttons(chat: "other").isEmpty)
    }

    @Test func serverMarksStayDistinct() {
        #expect(PluginTab.marks(["Home server", "Studio"]) == ["h", "s"])
        #expect(PluginTab.marks(["cvmini", "cvmac", "Studio"]) == ["1", "2", "3"])
        #expect(PluginTab.marks(["家", "Studio"]) == ["1", "2"])
    }
}
