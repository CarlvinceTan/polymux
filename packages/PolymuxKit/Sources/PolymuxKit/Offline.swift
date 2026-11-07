import Foundation

extension Error {
    /// True when the server couldn't be reached, as opposed to a request it refused.
    public var isOffline: Bool {
        guard let error = self as? URLError else { return false }
        return [.notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost, .cannotFindHost,
                .timedOut, .dnsLookupFailed, .dataNotAllowed, .internationalRoamingOff].contains(error.code)
    }

    /// True when the same request may succeed later, so it is safe to send again.
    public var isTransient: Bool {
        if isOffline { return true }
        if case let .response(status, _)? = self as? ClientError { return status >= 500 }
        return false
    }

    /// The server's own explanation of a refused request, otherwise the given wording.
    public func reason(or fallback: String) -> String {
        if case let .response(status, message)? = self as? ClientError, (400..<500).contains(status), !message.isEmpty { return message }
        return fallback
    }
}
