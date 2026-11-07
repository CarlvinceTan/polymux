import SwiftUI

struct ScrollDate: View {
    let date: Date

    var body: some View {
        Text(ScrollDay().format(date))
            .font(.caption.weight(.medium))
            .padding(.horizontal, 12).padding(.vertical, 6)
            .glassEffect(.regular, in: Capsule())
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

struct ScrollDay: FormatStyle {
    var now = Date()
    var calendar = Calendar.current
    var locale = Locale(identifier: "en_US")

    func format(_ value: Date) -> String {
        if calendar.isDate(value, inSameDayAs: now) { return "Today" }
        if let yesterday = calendar.date(byAdding: .day, value: -1, to: now),
           calendar.isDate(value, inSameDayAs: yesterday) { return "Yesterday" }
        let pattern: String
        if let week = calendar.dateInterval(of: .weekOfYear, for: now),
           value >= week.start && value < week.end {
            pattern = "EEEE"
        } else if calendar.component(.year, from: value) == calendar.component(.year, from: now) {
            pattern = "MMMM d"
        } else {
            pattern = "d MMMM yyyy"
        }
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = pattern
        return formatter.string(from: value)
    }
}

struct ScrollDateFrame: Equatable, Sendable {
    let date: Date
    let frame: CGRect
}

struct ScrollDateFrames: PreferenceKey {
    static let defaultValue: [String: ScrollDateFrame] = [:]

    static func reduce(value: inout [String: ScrollDateFrame],
                       nextValue: () -> [String: ScrollDateFrame]) {
        value.merge(nextValue(), uniquingKeysWith: { _, next in next })
    }
}
