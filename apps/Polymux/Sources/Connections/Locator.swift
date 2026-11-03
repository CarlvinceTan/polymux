import CoreLocation
import MapKit

/// This device's current location.
@MainActor
final class Locator: NSObject, CLLocationManagerDelegate {
    static let shared = Locator()

    private let manager = CLLocationManager()
    private var waiting: CheckedContinuation<CLAuthorizationStatus, Never>?

    struct Reading: Encodable {
        let latitude: Double
        let longitude: Double
        let accuracyMeters: Double
        let measuredAt: String
        let place: String?
        let address: String?
    }

    override private init() {
        super.init()
        manager.delegate = self
    }

    func authorize() async throws {
        var status = manager.authorizationStatus
        if status == .notDetermined {
            status = await withCheckedContinuation { continuation in
                waiting = continuation
                manager.requestWhenInUseAuthorization()
            }
        }
        guard Self.allowed(status) else {
            throw ConnectionError("Allow Polymux to use your location in Settings.")
        }
    }

    func current() async throws -> Data {
        guard Self.allowed(manager.authorizationStatus) else {
            throw ConnectionError("Location access is off for Polymux on this device.")
        }
        let location = try await limited(20, "Couldn't get a location fix in time.") {
            for try await update in CLLocationUpdate.liveUpdates() {
                if let location = update.location { return location }
            }
            throw ConnectionError("Location updates stopped.")
        }
        let item = try? await MKReverseGeocodingRequest(location: location)?.mapItems.first
        return try encoded(Reading(
            latitude: location.coordinate.latitude, longitude: location.coordinate.longitude,
            accuracyMeters: location.horizontalAccuracy,
            measuredAt: location.timestamp.ISO8601Format(),
            place: item?.name, address: item?.address?.fullAddress))
    }

    private static func allowed(_ status: CLAuthorizationStatus) -> Bool {
        #if os(iOS)
        status == .authorizedAlways || status == .authorizedWhenInUse
        #else
        status == .authorizedAlways
        #endif
    }

    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let status = manager.authorizationStatus
        guard status != .notDetermined else { return }
        Task { @MainActor in
            waiting?.resume(returning: status)
            waiting = nil
        }
    }
}
