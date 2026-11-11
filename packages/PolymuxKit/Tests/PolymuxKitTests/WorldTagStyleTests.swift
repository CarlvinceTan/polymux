import Testing
@testable import PolymuxKit

@Suite
struct WorldTagStyleTests {
    /// Every state a run can be in reads differently, and resting says nothing.
    @Test func everyStateWearsItsOwnTag() {
        let working = WorldTagStyle.tag(state: WorkNodeState.working, cargo: WorkKind.image, doing: "Drawing Mia", subtitle: nil)
        #expect(working?.symbol == WorkKind.look(WorkKind.image).symbol && working?.text == "Drawing Mia" && working?.tint == WorkKind.image)
        // With nothing said about the work, the kind of thing being carried names it.
        #expect(WorldTagStyle.tag(state: WorkNodeState.working, cargo: WorkKind.video, doing: nil, subtitle: nil)?.text == "Video")
        #expect(WorldTagStyle.tag(state: WorkNodeState.working, cargo: nil, doing: nil, subtitle: nil)?.text == "Working")
        #expect(WorldTagStyle.tag(state: WorkNodeState.waiting, cargo: nil, doing: nil, subtitle: nil)?.text == "Waiting for you")
        #expect(WorldTagStyle.tag(state: WorkNodeState.waiting, cargo: nil, doing: "Need a key", subtitle: nil)?.text == "Need a key")
        #expect(WorldTagStyle.tag(state: WorkNodeState.failed, cargo: nil, doing: nil, subtitle: "Build failed")?.text == "Failed")
        #expect(WorldTagStyle.tag(state: WorkNodeState.done, cargo: WorkKind.file, doing: "wrote it", subtitle: nil) == nil)
        #expect(WorldTagStyle.tag(state: WorkNodeState.idle, cargo: nil, doing: nil, subtitle: nil) == nil)
        #expect(WorldTagStyle.tag(state: nil, cargo: nil, doing: nil, subtitle: nil) == nil)
    }
}
