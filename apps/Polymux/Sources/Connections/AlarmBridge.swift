#if os(iOS)
import AlarmKit
import SwiftUI

/// Alarms that ring like Clock alarms, through AlarmKit.
@MainActor
enum AlarmBridge {
    struct Metadata: AlarmMetadata {}

    struct Entry: Encodable {
        let id: String
        let label: String?
        let time: String?
        let weekdays: [String]
        let state: String
    }

    private static let days: [String: Locale.Weekday] = [
        "monday": .monday, "tuesday": .tuesday, "wednesday": .wednesday, "thursday": .thursday,
        "friday": .friday, "saturday": .saturday, "sunday": .sunday,
    ]
    /// AlarmKit doesn't return titles, so labels are kept here by alarm ID.
    private static let labelsKey = "connections.alarmLabels"

    static func authorize() async throws {
        guard try await AlarmManager.shared.requestAuthorization() == .authorized else {
            throw ConnectionError("Allow Polymux to set alarms in Settings.")
        }
    }

    static func run(_ command: DeviceCommand) async throws -> Data {
        guard AlarmManager.shared.authorizationState == .authorized else {
            throw ConnectionError("Alarm access is off for Polymux on this device.")
        }
        var labels = UserDefaults.standard.dictionary(forKey: labelsKey) as? [String: String] ?? [:]
        switch command.action {
        case "list":
            let alarms = try AlarmManager.shared.alarms
            labels = labels.filter { id, _ in alarms.contains { $0.id.uuidString == id } }
            UserDefaults.standard.set(labels, forKey: labelsKey)
            return try encoded(alarms.map { entry($0, label: labels[$0.id.uuidString]) })
        case "set":
            let parts = (command.time ?? "").split(separator: ":").compactMap { Int($0) }
            guard parts.count == 2, (0..<24).contains(parts[0]), (0..<60).contains(parts[1]) else {
                throw ConnectionError("Give the time as HH:MM.")
            }
            let weekdays = try (command.weekdays ?? []).map {
                guard let day = days[$0] else { throw ConnectionError("Unknown weekday \($0).") }
                return day
            }
            let label = String((command.label ?? "Alarm").prefix(64))
            let schedule = Alarm.Schedule.relative(.init(time: .init(hour: parts[0], minute: parts[1]),
                                                         repeats: weekdays.isEmpty ? .never : .weekly(weekdays)))
            let attributes = AlarmAttributes<Metadata>(presentation: AlarmPresentation(alert: .init(title: LocalizedStringResource(stringLiteral: label))),
                                                       tintColor: Color("AccentColor"))
            let alarm = try await AlarmManager.shared.schedule(id: UUID(), configuration: .alarm(schedule: schedule, attributes: attributes))
            labels[alarm.id.uuidString] = label
            UserDefaults.standard.set(labels, forKey: labelsKey)
            return try encoded(entry(alarm, label: label))
        case "cancel":
            guard let id = command.id.flatMap(UUID.init(uuidString:)) else { throw ConnectionError("Choose an alarm ID from list.") }
            try AlarmManager.shared.cancel(id: id)
            labels[id.uuidString] = nil
            UserDefaults.standard.set(labels, forKey: labelsKey)
            return try encoded(["cancelled": id.uuidString])
        default:
            throw ConnectionError("Use list, set or cancel.")
        }
    }

    private static func entry(_ alarm: Alarm, label: String?) -> Entry {
        var time: String?
        var weekdays: [String] = []
        if case .relative(let relative) = alarm.schedule {
            time = String(format: "%02d:%02d", relative.time.hour, relative.time.minute)
            if case .weekly(let repeats) = relative.repeats {
                weekdays = days.filter { repeats.contains($0.value) }.map(\.key).sorted()
            }
        }
        return Entry(id: alarm.id.uuidString, label: label, time: time, weekdays: weekdays, state: "\(alarm.state)")
    }
}
#endif
