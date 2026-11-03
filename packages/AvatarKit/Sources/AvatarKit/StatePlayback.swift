public struct StatePlayback {
    private var state: StateID?
    private var cueID: String?
    private var controlled = false
    private var sampled = 0.0
    private var started = 0.0

    public init() {}

    public mutating func update(_ engine: BotEngine, state next: StateID, now: Double,
                                controlled: Bool, repeating: Bool, cueID: String?) {
        let newCue = cueID != nil && cueID != self.cueID
        if state == nil {
            engine.reset(next, controlled ? 0 : now)
            started = now
        } else if controlled {
            if !self.controlled || state != next || newCue { engine.reset(next, 0) }
        } else {
            // Rebase a held pose before moving back to the live clock.
            if self.controlled, let state { engine.reset(state, now - sampled) }
            if state != next || newCue {
                engine.setState(next, now, restart: state == next)
                started = now
            } else if self.controlled {
                started = now - sampled
            }
            if repeating, [.swirl, .orbit, .play].contains(next),
               let duration = STATE_BY_ID[next]?.duration, now - started >= duration {
                engine.setState(next, now, restart: true)
                started = now
            }
        }
        state = next
        self.cueID = cueID
        self.controlled = controlled
        sampled = controlled ? now : now - started
    }
}
