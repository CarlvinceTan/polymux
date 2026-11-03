import ActivityKit
import AppIntents
import PolymuxCall
import SwiftUI
import WidgetKit

@main
struct CallBundle: WidgetBundle {
    var body: some Widget { CallWidget() }
}

struct CallWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: CallAttributes.self) { context in
            CallBanner(name: context.attributes.name, state: context.state)
                .padding(16)
                .activityBackgroundTint(nil)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label(context.attributes.name, systemImage: "phone.fill")
                        .font(.headline).lineLimit(1)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    CallTimer(started: context.state.started).font(.headline)
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(label(context.state)).font(.subheadline).foregroundStyle(.secondary)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    EndButton()
                }
            } compactLeading: {
                Image(systemName: context.state.muted ? "mic.slash.fill" : "phone.fill").foregroundStyle(.green)
            } compactTrailing: {
                CallTimer(started: context.state.started).font(.caption2).lineLimit(1).minimumScaleFactor(0.5).frame(maxWidth: 44)
            } minimal: {
                Image(systemName: "phone.fill").foregroundStyle(.green)
            }
        }
    }
}

private func label(_ state: CallAttributes.ContentState) -> String {
    state.muted ? "Microphone muted" : state.status
}

private struct CallTimer: View {
    let started: Date?

    var body: some View {
        if let started {
            Text(started, style: .timer).monospacedDigit().multilineTextAlignment(.trailing)
        } else {
            Text("Calling")
        }
    }
}

private struct EndButton: View {
    var body: some View {
        Button(intent: EndCall()) {
            Label("End", systemImage: "phone.down.fill")
                .font(.subheadline.weight(.semibold))
                .frame(maxWidth: .infinity)
        }
        .tint(.red)
        .buttonStyle(.borderedProminent)
    }
}

private struct CallBanner: View {
    let name: String
    let state: CallAttributes.ContentState

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Label(name, systemImage: "phone.fill").font(.headline).lineLimit(1)
                Text(label(state)).font(.subheadline).foregroundStyle(.secondary)
                CallTimer(started: state.started).font(.subheadline).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            Button(intent: EndCall()) {
                Image(systemName: "phone.down.fill").frame(width: 44, height: 44)
            }
            .tint(.red)
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.circle)
            .accessibilityLabel("End call")
        }
    }
}
