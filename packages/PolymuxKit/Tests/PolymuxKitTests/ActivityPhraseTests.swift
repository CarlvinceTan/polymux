import Foundation
import Testing
@testable import PolymuxKit

@Suite @MainActor struct ActivityPhraseTests {
    private func running(_ tool: String?, status: String? = nil) -> Run {
        var run = Run(id: "r", conversationID: "c", trigger: "message", attempt: 1, seq: 1, createdAt: "2026-10-09T12:00:00Z",
                      updatedAt: "2026-10-09T12:00:00Z", activity: tool == nil ? "generating" : "tool", agentID: "a",
                      status: "running", summary: "Using tools", statusText: status)
        run.tools = tool.map { [ToolActivity(id: "t", runID: "r", name: $0, status: ToolActivityStatus.running)] }
        return run
    }

    @Test func aToolGetsItsOwnPhrase() {
        let view = ActivityStore.present(runs: [running("http_request")], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.line([("Scout", view)]) == "Doing some reading")
    }

    @Test func aSetStatusReplacesTheToolPhrase() {
        let view = ActivityStore.present(runs: [running("web_search", status: "Searching for Artemis II dates")], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.line([("Scout", view)]) == "Searching for Artemis II dates")
    }

    @Test func aSetStatusReplacesTheThinkingPhrase() {
        let view = ActivityStore.present(runs: [running(nil, status: "Drafting a greeting")], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.line([("Scout", view)]) == "Drafting a greeting")
    }

    @Test func typingHidesTheSetStatus() {
        let view = ActivityStore.present(runs: [running("web_search", status: "Searching for Artemis II dates")],
                                         conversationID: "c", agentID: "a", streaming: true)
        #expect(ActivityPhrase.line([("Scout", view)]) == "Typing…")
    }

    @Test func aGroupShowsTheActiveStatus() {
        let status = ActivityStore.present(runs: [running("web_search", status: "Searching for dates")], conversationID: "c", agentID: "a")
        let working = ActivityView(working: true, phrase: "Doing some reading")
        #expect(ActivityPhrase.line([("Ada", status), ("Bo", working)]) == "Searching for dates…")
    }

    @Test func thinkingTurnsToTypingOnceTextStreams() {
        let thinking = ActivityStore.present(runs: [running(nil)], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.thinking.contains(ActivityPhrase.line([("Scout", thinking)])))
        let streaming = ActivityStore.present(runs: [running(nil)], conversationID: "c", agentID: "a", streaming: true)
        #expect(ActivityPhrase.line([("Scout", streaming)]) == "Typing…")
    }

    @Test func aThreadListingKeepsTheRunningTool() {
        let store = ActivityStore()
        let run = running("bash")
        store.receive(Event(seq: 1, conversationID: "c", type: EventType.toolStart, agentID: "a", createdAt: run.createdAt, run: run), live: true)
        var listed = run
        listed.tools = []
        store.restore([listed])
        #expect(ActivityPhrase.line([("Reader", store.view(conversationID: "c", agentID: "a"))]) == "Running a few commands")
    }

    @Test(arguments: [
        ("web_search", "Digging around"), ("WebFetch", "Doing some reading"),
        ("functions.read_file", "Reading the file"), ("mcp__filesystem__read_file", "Reading the file"),
        ("apply_patch", "Jotting it down"), ("BoxShell", "Running a few commands"),
        ("MachineShell", "Working on your computer"), ("CopyToBox", "Moving files over"),
        ("AwaitShell", "Waiting on results"), ("generate_image", "Sketching something up"),
        ("GenerateVideo", "Filming a clip"), ("CloudAgent", "Writing some code"),
        ("Screenshot", "Clicking around"), ("browser_click", "Clicking through pages"),
        ("message_agent", "Checking with another Bot"), ("send_to_user", "Sending a message"),
        ("request_user_form", "Needs your input"), ("CheckSubscriptionUsage", "Checking your usage"),
        ("manage_integrations", "Working in a connected app"), ("mcp__calendar__events", "Working in a connected app")
    ])
    func toolPhrasesMatchTheAction(tool: String, phrase: String) {
        let view = ActivityStore.present(runs: [running(tool)], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.line([("Scout", view)]) == phrase)
    }

    @Test func unknownToolsKeepAStableWorkingPhrase() {
        let view = ActivityStore.present(runs: [running("unrecognized")], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.working.contains(ActivityPhrase.line([("Scout", view)])))
        #expect(ActivityPhrase.tool("unrecognized") == nil)
    }

    @Test func theSelectedToolWinsOverOtherRunningTools() {
        var run = running("read_file")
        run.toolID = "t"
        run.tools?.append(ToolActivity(id: "other", runID: "r", name: "bash", status: ToolActivityStatus.running))
        let view = ActivityStore.present(runs: [run], conversationID: "c", agentID: "a")
        #expect(ActivityPhrase.line([("Scout", view)]) == "Reading the file")
    }

    @Test func severalBotsAreNamed() {
        let working = ActivityView(working: true, phrase: "Doing some reading")
        let typing = ActivityView(typing: true, working: true)
        #expect(ActivityPhrase.line([("Ada", typing), ("Bo", typing)]) == "Ada and Bo are typing…")
        #expect(ActivityPhrase.line([("Ada", working), ("Bo", typing), ("Cy", typing)]) == "Ada, Bo and Cy are working…")
        #expect(ActivityPhrase.line([("Ada", working), ("Bo", working), ("Cy", working), ("Di", working)])
                == "Ada, Bo and 2 others are working…")
    }
}
