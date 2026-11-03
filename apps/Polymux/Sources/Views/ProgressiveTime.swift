import Foundation

struct ProgressiveTime: FormatStyle {
    var separator = false
    var now = Date()

    func format(_ value: Date) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_SG")
        calendar.firstWeekday = 2
        calendar.minimumDaysInFirstWeek = 4
        let hour = calendar.component(.hour, from: value)
        let minute = calendar.component(.minute, from: value)
        let time = "\(hour % 12 == 0 ? 12 : hour % 12).\(String(format: "%02d", minute)) \(hour < 12 ? "am" : "pm")"
        let day: String
        if calendar.isDate(value, inSameDayAs: now) {
            return time
        } else if let yesterday = calendar.date(byAdding: .day, value: -1, to: now),
                  calendar.isDate(value, inSameDayAs: yesterday) {
            day = "Yesterday"
        } else if let week = calendar.dateInterval(of: .weekOfYear, for: now), week.contains(value) {
            day = value.formatted(.dateTime.weekday(.wide))
        } else if calendar.component(.year, from: value) == calendar.component(.year, from: now) {
            if separator {
                day = value.formatted(.dateTime.day().month(.abbreviated).locale(Locale(identifier: "en_GB")))
            } else {
                return String(format: "%02d/%02d", calendar.component(.day, from: value), calendar.component(.month, from: value))
            }
        } else {
            day = value.formatted(.dateTime.day().month(.abbreviated).year().locale(Locale(identifier: "en_GB")))
        }
        return separator ? "\(day)\(day == "Yesterday" || weekDay(value, now: now, calendar: calendar) ? " " : ", ")\(time)" : day
    }

    private func weekDay(_ value: Date, now: Date, calendar: Calendar) -> Bool {
        calendar.dateInterval(of: .weekOfYear, for: now)?.contains(value) ?? false
    }
}
