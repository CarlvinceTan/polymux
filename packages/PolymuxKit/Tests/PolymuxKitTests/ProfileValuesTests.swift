import Foundation
import Testing
@testable import PolymuxKit

struct ProfileValuesTests {
    private let saved = ProfileValues(name: "New Bot", role: "")
    private let update = ProfileValues(name: "Alan", role: "Researcher")

    @Test func cleanFieldsRefresh() {
        #expect(saved.merging(update, saved: saved) == update)
    }

    @Test func nameEditsSurvive() {
        let edit = ProfileValues(name: "Local name", role: "")
        #expect(edit.merging(update, saved: saved) == ProfileValues(name: "Local name", role: "Researcher"))
    }

    @Test func roleEditsSurvive() {
        let edit = ProfileValues(name: "New Bot", role: "Local role")
        #expect(edit.merging(update, saved: saved) == ProfileValues(name: "Alan", role: "Local role"))
    }

    @Test func bothEditsSurvive() {
        let edit = ProfileValues(name: "Local name", role: "Local role")
        #expect(edit.merging(update, saved: saved) == edit)
    }

    @Test func promptEditsSurvive() {
        let saved = ProfileValues(name: "New Bot", role: "", prompt: "Old")
        let edit = ProfileValues(name: "New Bot", role: "", prompt: "Local prompt\n")
        let update = ProfileValues(name: "Alan", role: "Researcher", prompt: "Server prompt")
        #expect(edit.merging(update, saved: saved) == ProfileValues(name: "Alan", role: "Researcher", prompt: "Local prompt\n"))
    }

    @Test func whitespaceRefreshes() {
        let edit = ProfileValues(name: " New Bot\n", role: " ")
        #expect(edit.merging(update, saved: saved) == update)
    }

    @Test func unchangedRefreshPreservesSpaces() {
        let saved = ProfileValues(name: "Linus", role: "Maintainer", prompt: "Instructions")
        let edit = ProfileValues(name: "Linus ", role: "Maintainer ", prompt: "Instructions\n")
        #expect(edit.merging(saved, saved: saved) == edit)
    }

    @Test func acknowledgementPreservesSpaces() {
        let edit = ProfileValues(name: "Alan ", role: "Researcher ")
        #expect(edit.merging(update, saved: update) == edit)
    }

    @Test func acknowledgementSettles() {
        #expect(update.merging(update, saved: saved) == update)
    }

    @Test func draftDecodes() throws {
        let data = Data(#"{"name":"Local name","role":"Local role","prompt":"Local prompt"}"#.utf8)
        #expect(try JSONDecoder().decode(ProfileValues.self, from: data) == ProfileValues(name: "Local name", role: "Local role", prompt: "Local prompt"))
    }
}
