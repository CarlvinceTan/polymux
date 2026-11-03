import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct InstantUITests {
    @Test func intervalReturnsTheBodyAndUsesBothBudgets() {
        let trace = SignpostTrace()
        let value = InstantUI.interval("return", budget: InstantUI.stateBudget, trace: trace, from: ContinuousClock().now) {
            "shown"
        }
        #expect(value == "shown")
        #expect(InstantUI.feedbackBudget == .milliseconds(16))
        #expect(InstantUI.stateBudget == .milliseconds(50))
        let span = trace.spans.first
        #expect(span?.name == "return")
        #expect(span?.budget == InstantUI.stateBudget)
        #expect(span?.withinBudget == true)
        #expect((span?.duration ?? .seconds(1)) < InstantUI.stateBudget)
    }

    @Test func tyingTheBudgetIsAMiss() {
        let tie = SignpostTrace.Span(name: "tie", duration: InstantUI.feedbackBudget, budget: InstantUI.feedbackBudget)
        let inside = SignpostTrace.Span(name: "inside", duration: .milliseconds(15), budget: InstantUI.feedbackBudget)
        #expect(tie.withinBudget == false)
        #expect(inside.withinBudget == true)
        let trace = SignpostTrace()
        trace.record("tie", duration: InstantUI.stateBudget, budget: InstantUI.stateBudget)
        #expect(trace.meetsBudgets == false)
    }

    @Test func stalledIntervalReportsAMiss() {
        let trace = SignpostTrace()
        let tap = ContinuousClock().now
        pause(.milliseconds(70))
        let value = InstantUI.interval("late", budget: InstantUI.stateBudget, trace: trace, from: tap) { 7 }
        let span = trace.spans.first
        #expect(value == 7)
        #expect(span?.withinBudget == false)
        #expect((span?.duration ?? .zero) > InstantUI.stateBudget)
        #expect(trace.meetsBudgets == false)
    }
}

private func pause(_ duration: Duration) {
    let clock = ContinuousClock()
    let start = clock.now
    while start.duration(to: clock.now) < duration {}
}
