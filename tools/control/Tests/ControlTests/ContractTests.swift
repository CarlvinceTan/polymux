import Foundation
import Testing
@testable import Control

@Test func requestValidation() throws {
    for input in ["", "{", "[]", "null", "{\"pid\":123}", "{\"instance\":3}", "{\"window\":-1}",
                  "{\"maxElements\":0}", "{\"maxDepth\":101}", "{\"budgetMS\":5001}"] {
        #expect(throws: (any Error).self) { try Request(data: Data(input.utf8)) }
    }
    let request = try Request(data: Data("{\"window\":123,\"maxElements\":1,\"maxDepth\":1,\"budgetMS\":100}".utf8))
    #expect(try request.windowID() == 123)
    #expect(throws: Refusal.self) { try request.instancePath() }
    #expect(throws: Refusal.self) { try Request(data: Data("{}".utf8)).windowID() }
}

@Test func instanceScope() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).resolvingSymlinksInPath()
    try FileManager.default.createDirectory(at: root.appendingPathComponent("1"), withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: root) }
    #expect(try InstancePath(root.appendingPathComponent("1").path, base: root).number == 1)
    for suffix in ["0", "01", "-1", "personal", "1/nested"] {
        #expect(throws: Refusal.self) { try InstancePath(root.appendingPathComponent(suffix).path, base: root) }
    }
    #expect(throws: Refusal.self) { try InstancePath("1", base: root) }
    #expect(throws: Refusal.self) { try InstancePath(root.deletingLastPathComponent().appendingPathComponent("1").path, base: root) }
    try FileManager.default.createSymbolicLink(at: root.appendingPathComponent("2"), withDestinationURL: root.appendingPathComponent("1"))
    #expect(throws: Refusal.self) { try InstancePath(root.appendingPathComponent("2").path, base: root) }
}

@Test func readBudgetsNeverTreatPartialAsEmpty() {
    var elements = ReadBudget(elements: 1, depth: 3, milliseconds: 1000)
    let admitted = elements.admit(depth: 0)
    let overflow = elements.admit(depth: 1)
    #expect(admitted)
    #expect(!overflow)
    #expect(elements.reasons == ["maxElements"])
    var depth = ReadBudget(elements: 10, depth: 1, milliseconds: 1000)
    let tooDeep = depth.admit(depth: 2)
    #expect(!tooDeep)
    #expect(depth.reasons == ["maxDepth"])
    var expired = ReadBudget(elements: 10, depth: 3, milliseconds: -1)
    let tooLate = expired.admit(depth: 0)
    #expect(!tooLate)
    #expect(expired.reasons == ["deadline"])
    expired.incomplete("axRead")
    #expect(expired.reasons == ["deadline", "axRead"])
}

@Test func processGenerationRejectsMissingProcess() {
    #expect(throws: Refusal.self) { try ProcessStamp(-1) }
    #expect(throws: Refusal.self) { try ProcessStamp(Int32.max) }
}

@Test func sessionRequestsRejectInvalidFieldsAndBudgets() throws {
    for input in ["[]", "null", "{\"op\":\"press\",\"pid\":123}", "{\"op\":\"snapshot\",\"maxElements\":0}",
                  "{\"op\":\"scroll\",\"amount\":51}", "{\"op\":3}"] {
        #expect(throws: (any Error).self) { try SessionRequest(Data(input.utf8)) }
    }
    let request = try SessionRequest(Data("{\"op\":\"press\",\"token\":\"session:1:2\"}".utf8))
    #expect(request.token == "session:1:2")
    #expect(throws: (any Error).self) {
        try SessionRequest(JSONSerialization.data(withJSONObject: ["op": "set", "text": String(repeating: "x", count: 16385)]))
    }
}
