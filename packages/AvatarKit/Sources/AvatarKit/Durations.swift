import Foundation

public let MIN_BLOCK = STATES.map(\.morph).max()!
public let MAX_BLOCK = 10.0
public let STEP = 0.1

public func minDurationOf(_ state: StateID) -> Double {
    max(MIN_BLOCK, STATE_BY_ID[state]?.minDuration ?? MIN_BLOCK)
}

public func clampDuration(_ state: StateID, _ seconds: Double) -> Double {
    let snapped = floor(seconds / STEP + 0.5) * STEP
    let bounded = min(MAX_BLOCK, max(minDurationOf(state), snapped))
    return floor(bounded * 100 + 0.5) / 100
}
