import SwiftUI
import PolymuxKit

struct VoiceOverlay: View {
    @State private var store: any VoiceSession
    @State private var closing = false
    @State private var retrying = false
    @State private var retryTask: Task<Void, Never>?
    @State private var lifetime: VoiceLifetime?
    @State private var owner = UUID()
    private let dismiss: () -> Void

    @MainActor
    init(store: any VoiceSession, dismiss: @escaping () -> Void) {
        _store = State(initialValue: store)
        self.dismiss = dismiss
    }

    var body: some View {
        VStack(spacing: 0) {
            Spacer(minLength: 0)
            VStack(spacing: 12) {
                Text(status)
                    .font(.title2).foregroundStyle(.tint)
                    .accessibilityValue(caption ?? "")
                if let caption {
                    Text(caption)
                        .font(.body).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center).lineLimit(4)
                }
                if let issue = store.issue {
                    Text(issue)
                        .font(.caption).foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                    Button("Try again", action: retry)
                        .buttonStyle(.glass)
                        .disabled(retrying || closing)
                }
            }
            .frame(maxWidth: 400).padding(.horizontal, 24)
            Spacer(minLength: 0)
            HStack(alignment: .top, spacing: 40) {
                control(store.silenced ? "speaker.slash.fill" : "speaker.wave.2.fill",
                        store.silenced ? "Speaker off" : "Speaker", action: store.toggleSilence)
                control("phone.down.fill", "End", fill: .red, ink: .white, action: close)
                control(store.muted ? "mic.slash.fill" : "mic.fill",
                        store.muted ? "Unmute" : "Mute", action: mute)
                    .disabled(store.phase == .failed)
            }
            .padding(.bottom, 24)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.background)
        .task {
            guard !closing else { return }
            let lifetime = VoiceLifetime.shared(store)
            self.lifetime = lifetime
            await lifetime.start(owner)
        }
        .onDisappear {
            retryTask?.cancel()
            lifetime?.stop(owner)
        }
        .onKeyPress(.escape) { close(); return .handled }
    }

    private func control(_ icon: String, _ label: String, fill: Color? = nil, ink: Color = .primary,
                         action: @escaping () -> Void) -> some View {
        VStack(spacing: 8) {
            Button(action: action) {
                Image(systemName: icon)
                    .font(.system(size: 26))
                    .foregroundStyle(ink)
                    .frame(width: 72, height: 72)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .background(fill ?? .clear, in: Circle())
            .glassEffect(fill == nil ? .regular.interactive() : .identity, in: Circle())
            .disabled(closing)
            .accessibilityLabel(label)
            Text(label).font(.footnote).foregroundStyle(.secondary)
        }
    }

    private var status: String {
        if closing { return "Ending speech" }
        if store.muted { return "Microphone muted" }
        switch store.phase {
        case .preparing: return "Preparing speech"
        case .failed: return "Speech unavailable"
        case .listening: return "Listening"
        case .thinking: return "Thinking"
        case .speaking: return "Speaking"
        case .interrupted: return "Interrupted"
        }
    }

    private var caption: String? {
        guard !closing, !store.muted, store.phase == .listening, !store.transcript.isEmpty else {
            return nil
        }
        return store.transcript
    }

    private func mute() { store.toggleMute() }

    private func retry() {
        guard !retrying, !closing else { return }
        retrying = true
        retryTask = Task {
            await store.retry()
            retrying = false
        }
    }

    private func close() {
        guard !closing else { return }
        closing = true
        retryTask?.cancel()
        lifetime?.stop(owner)
        dismiss()
    }
}

@MainActor
private final class VoiceLifetime {
    private static let lifetimes = NSMapTable<AnyObject, VoiceLifetime>(keyOptions: .weakMemory, valueOptions: .strongMemory)
    private static weak var active: VoiceLifetime?
    private static var shutdown: Task<Void, Never>?
    private weak var store: (any VoiceSession)?
    private var owner: UUID?
    private var cleanup: Task<Void, Never>?

    private init(_ store: any VoiceSession) { self.store = store }

    static func shared(_ store: any VoiceSession) -> VoiceLifetime {
        if let lifetime = lifetimes.object(forKey: store) { return lifetime }
        let lifetime = VoiceLifetime(store)
        lifetimes.setObject(lifetime, forKey: store)
        return lifetime
    }

    func start(_ owner: UUID) async {
        guard !Task.isCancelled else { return }
        if let previous = Self.active, let previousOwner = previous.owner, previousOwner != owner {
            previous.stop(previousOwner)
        }
        self.owner = owner
        Self.active = self
        await Self.shutdown?.value
        await cleanup?.value
        guard self.owner == owner, Self.active === self, !Task.isCancelled, let store else { return }
        await store.start()
    }

    func stop(_ owner: UUID) {
        guard self.owner == owner, let store else { return }
        self.owner = nil
        if Self.active === self { Self.active = nil }
        let previous = Self.shutdown
        let task = Task { await previous?.value; await store.stop() }
        cleanup = task
        Self.shutdown = task
    }
}
