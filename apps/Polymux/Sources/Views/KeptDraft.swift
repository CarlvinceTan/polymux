import SwiftUI
import PolymuxKit

/// Keeps a form's unsent input on this device as it changes, so a crash or quit doesn't lose it,
/// and puts it back when the form opens again.
struct KeptDraft<Value: Codable & Equatable & Sendable>: ViewModifier {
    let key: String
    @Binding var value: Value
    let cache: LocalCacheActor
    @State private var restored = false

    func body(content: Content) -> some View {
        content
            .task {
                if let saved: Value = await cache.snapshot(key) { value = saved }
                restored = true
            }
            .onChange(of: value) { _, current in
                guard restored else { return }
                Task { await cache.saveSnapshot(key, current) }
            }
    }

    static func discard(_ key: String, matching value: Value, cache: LocalCacheActor) {
        Task {
            try? await cache.call { saved in
                let current: Value? = try saved.snapshot(key)
                if current == value { try saved.saveSnapshot(key, Value?.none) }
            }
        }
    }

    /// Forgets the input once it's sent or the form is cancelled.
    static func discard(_ key: String, cache: LocalCacheActor) {
        Task { await cache.saveSnapshot(key, Value?.none) }
    }
}

extension View {
    func keepDraft<Value: Codable & Equatable & Sendable>(_ key: String, value: Binding<Value>, cache: LocalCacheActor) -> some View {
        modifier(KeptDraft(key: key, value: value, cache: cache))
    }
}
