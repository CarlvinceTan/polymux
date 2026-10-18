import Testing

// Keep deliberate main-actor stalls out of other tests’ wall-clock spans.
@Suite(.serialized)
struct InstantTiming {}
