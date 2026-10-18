import Foundation
#if os(macOS)
import Darwin
#endif

public struct APIClient: Sendable {
    public let baseURL: URL
    private let session: URLSession
    private let credential: Task<String?, Never>
    var token: String? { get async { await credential.value } }

    public init(baseURL: URL, session: URLSession = .shared, token: String? = nil) {
        self.init(baseURL: baseURL, session: session, token: token, discover: Self.discoverToken)
    }

    init(baseURL: URL, session: URLSession, token: String?, discover: @escaping @Sendable (URL) -> String?) {
        self.baseURL = baseURL
        self.session = session
        credential = Task.detached { token ?? discover(baseURL) }
    }

    public func resolve(_ path: String) -> URL? {
        URL(string: path, relativeTo: baseURL)?.absoluteURL
    }

    public func agents() async throws -> [Agent] {
        try await get("agents")
    }

    public func network() async throws -> NetworkInfo {
        try await get("network")
    }

    public func invite() async throws -> Invite {
        try await get("network/invite")
    }

    public func renameDevice(_ name: String) async throws {
        var request = try await request("network/devices/self")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["name": name])
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func removeDevice(_ id: String) async throws {
        var request = try await request("network/devices/\(id)")
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func notices(after: Int64? = nil) async throws -> NoticePage {
        try await get("notifications", query: after.map { [URLQueryItem(name: "after", value: String($0))] } ?? [])
    }

    public func registerPush(_ device: PushDevice) async throws -> PushStatus {
        var request = try await request("notifications/device")
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(device)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(PushStatus.self, from: data)
    }

    public func settings() async throws -> Settings {
        try await get("settings")
    }

    public func updateSettings(_ patch: SettingsPatch) async throws -> Settings {
        var request = try await request("settings")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(patch)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Settings.self, from: data)
    }

    public func speechKey(provider: String, key: String?) async throws -> Settings {
        guard provider == "gemini" else { throw ClientError.invalidURL }
        var request = try await request("settings/keys/\(provider)")
        request.httpMethod = key == nil ? "DELETE" : "PUT"
        if let key {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONEncoder().encode(SpeechKey(key: key))
        }
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Settings.self, from: data)
    }

    func speechText(_ body: Data) async throws -> Data {
        var request = try await request("speech/text")
        request.httpMethod = "POST"
        request.timeoutInterval = 50
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return data
    }

    func speechSocket() throws -> URL {
        var parts = URLComponents(url: baseURL.appendingPathComponent("speech/live"), resolvingAgainstBaseURL: false)
        guard parts?.scheme == "http" || parts?.scheme == "https" else { throw ClientError.invalidURL }
        let scheme = parts?.scheme == "https" ? "wss" : "ws"
        parts?.scheme = scheme
        parts?.query = nil
        guard let url = parts?.url else { throw ClientError.invalidURL }
        return url
    }

    public func createAgent(_ contact: CreateAgentRequest) async throws -> Agent {
        var request = try await request("agents")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(contact)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Agent.self, from: data)
    }

    public func agentProfile(id: String) async throws -> AgentProfile {
        try await get("agents/\(id)")
    }

    @discardableResult
    public func deleteAgent(id: String) async throws -> DeleteResponse {
        var request = try await request("agents/\(id)")
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(DeleteResponse.self, from: data)
    }

    public func updateAgent(id: String, request update: UpdateAgentRequest) async throws -> AgentProfile {
        var request = try await request("agents/\(id)")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(update)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(AgentProfile.self, from: data)
    }

    public func updateAvatar(id: String, request design: AvatarDesign) async throws -> Agent {
        var request = try await request("agents/\(id)/avatar")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(design)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Agent.self, from: data)
    }

    public func threads() async throws -> [Thread] {
        try await get("threads")
    }

    public func routines(agentID: String) async throws -> [Routine] {
        try await get("agents/\(agentID)/routines")
    }

    public func proactivity(agentID: String) async throws -> Proactivity {
        try await get("agents/\(agentID)/proactivity")
    }

    /// Cancels a routine the agent created. Cheap to redo, so it applies immediately.
    public func deleteRoutine(agentID: String, routineID: String) async throws -> DeleteResponse {
        var request = try await request("agents/\(agentID)/routines/\(routineID)")
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(DeleteResponse.self, from: data)
    }

    /// Asks the server to post the deterministic first-run card; the server decides whether it applies.
    public func requestFocus(agentID: String) async throws -> FocusStatus {
        try await post("agents/\(agentID)/focus")
    }

    public func answerFocus(agentID: String, optionID: String) async throws -> FocusStatus {
        try await post("agents/\(agentID)/focus/answer", FocusAnswer(optionID: optionID))
    }

    private func post<Value: Decodable>(_ path: String) async throws -> Value {
        var request = try await request(path)
        request.httpMethod = "POST"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Value.self, from: data)
    }

    private func post<Body: Encodable, Value: Decodable>(_ path: String, _ body: Body) async throws -> Value {
        var request = try await request(path)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Value.self, from: data)
    }

    public func setProactivity(agentID: String, settings: Proactivity) async throws -> Proactivity {
        var request = try await request("agents/\(agentID)/proactivity")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(settings)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Proactivity.self, from: data)
    }

    public func createThread(agentIDs: [String], title: String? = nil, leadAgentID: String? = nil) async throws -> Thread {
        var request = try await request("threads")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(CreateThreadRequest(leadAgentID: leadAgentID, agentIDs: agentIDs, title: title))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Thread.self, from: data)
    }

    public func updateThread(id: String, title: String? = nil, agentIDs: [String]? = nil, leadAgentID: String? = nil, pinned: Bool? = nil, pinOrder: Double? = nil) async throws -> Thread {
        var request = try await request("threads/\(id)")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(UpdateThreadRequest(leadAgentID: leadAgentID, agentIDs: agentIDs, title: title, pinned: pinned, pinOrder: pinOrder))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Thread.self, from: data)
    }

    public func messages(threadID: String, before: Int64? = nil) async throws -> [Message] {
        var query = [URLQueryItem(name: "limit", value: "100")]
        if let before { query.append(URLQueryItem(name: "before", value: String(before))) }
        return try await get("threads/\(threadID)/messages", query: query)
    }

    public func toolRuns(threadID: String, before: Int64? = nil) async throws -> [Run] {
        var query = [URLQueryItem(name: "limit", value: "100")]
        if let before { query.append(URLQueryItem(name: "before", value: String(before))) }
        return try await get("threads/\(threadID)/tool-runs", query: query)
    }

    public func events(threadID: String, after: Int64) async throws -> [Event] {
        try await get("threads/\(threadID)/events", query: [URLQueryItem(name: "after", value: String(after)), URLQueryItem(name: "limit", value: "1000")])
    }

    public func send(threadID: String, request: SendRequest) async throws {
        try await post("threads/\(threadID)/messages", body: request)
    }

    public func voiceTask(threadID: String, request task: VoiceRequest) async throws -> VoiceResponse {
        var request = try await request("threads/\(threadID)/voice")
        request.httpMethod = "POST"
        request.timeoutInterval = 10 * 60
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(task)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(VoiceResponse.self, from: data)
    }

    public func markRead(threadID: String, seq: Int64) async throws {
        try await post("threads/\(threadID)/read", body: ReadRequest(seq: seq))
    }

    public func stop(threadID: String, withdrawQueued: Bool = false) async throws {
        try await post("threads/\(threadID)/stop", body: ReadRequest(), query: withdrawQueued ? [URLQueryItem(name: "queued", value: "1")] : [])
    }

    public func search(threadID: String, query: String) async throws -> [SearchHit] {
        try await get("threads/\(threadID)/search", query: [URLQueryItem(name: "q", value: query)])
    }

    public func upload(file: URL) async throws -> Attachment {
        let boundary = UUID().uuidString
        let data = try Data(contentsOf: file)
        let name = file.lastPathComponent.replacingOccurrences(of: "\"", with: "")
            .replacingOccurrences(of: "\r", with: "").replacingOccurrences(of: "\n", with: "")
        var body = Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"\(name)\"\r\nContent-Type: application/octet-stream\r\n\r\n".utf8)
        body.append(data)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        var request = try await request("attachments")
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        let (response, metadata) = try await session.upload(for: request, from: body)
        try validate(metadata, data: response)
        return try JSONDecoder().decode(Attachment.self, from: response)
    }

    public func stream(threadID: String, after: Int64) -> AsyncThrowingStream<Event, any Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try await request("threads/\(threadID)/stream", query: [URLQueryItem(name: "after", value: String(after))])
                    request.timeoutInterval = 60 * 60
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    let (bytes, response) = try await session.bytes(for: request)
                    try validate(response)
                    var parser = SSEParser()
                    for try await byte in bytes {
                        try Task.checkCancellation()
                        if let event = try parser.feed(byte) { continuation.yield(event) }
                    }
                    if let event = try parser.finish() { continuation.yield(event) }
                    continuation.finish()
                } catch { continuation.finish(throwing: error) }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func get<T: Decodable>(_ path: String, query: [URLQueryItem] = []) async throws -> T {
        let (data, response) = try await session.data(for: request(path, query: query))
        try validate(response, data: data)
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func post<T: Encodable>(_ path: String, body: T, query: [URLQueryItem] = []) async throws {
        var request = try await request(path, query: query)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    private func request(_ path: String, query: [URLQueryItem] = []) async throws -> URLRequest {
        var parts = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)
        if !query.isEmpty { parts?.queryItems = query }
        guard let url = parts?.url else { throw ClientError.invalidURL }
        var request = URLRequest(url: url)
        request.timeoutInterval = 20
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        if let token = await credential.value { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        try Task.checkCancellation()
        return request
    }

    private static func discoverToken(_ url: URL) -> String? {
        #if os(macOS)
        let host = url.host?.lowercased() ?? ""
        var name = [CChar](repeating: 0, count: Int(NI_MAXHOST))
        guard gethostname(&name, name.count) == 0 else { return nil }
        let local = String(cString: name).lowercased().components(separatedBy: ".")[0]
        let localHost = ["localhost", "127.0.0.1", "::1", local, "\(local).local"].contains(host)
            || localAddresses().contains(host)
            || (host.hasSuffix(".ts.net") && !Set(Host(name: host).addresses).isDisjoint(with: localAddresses()))
        if localHost {
            let directory = ProcessInfo.processInfo.environment["POLYMUX_DATA_DIR"].map {
                URL(fileURLWithPath: $0, isDirectory: true)
            } ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".polymux/data")
            if let token = try? String(contentsOf: directory.appendingPathComponent("devices/local-token"), encoding: .utf8) {
                return token
            }
        }
        return try? String(contentsOf: ClientPaths.token(url.absoluteString), encoding: .utf8)
        #else
        return try? String(contentsOf: ClientPaths.token(url.absoluteString), encoding: .utf8)
        #endif
    }

    #if os(macOS)
    private static func localAddresses() -> Set<String> {
        var interfaces: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&interfaces) == 0 else { return [] }
        defer { freeifaddrs(interfaces) }
        var addresses = Set<String>()
        var next = interfaces
        while let current = next {
            next = current.pointee.ifa_next
            guard let address = current.pointee.ifa_addr else { continue }
            var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            if getnameinfo(address, socklen_t(address.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 {
                addresses.insert(String(cString: host))
            }
        }
        return addresses
    }
    #endif

    private func validate(_ response: URLResponse, data: Data = Data()) throws {
        guard let http = response as? HTTPURLResponse else { throw ClientError.unavailable }
        guard (200..<300).contains(http.statusCode) else {
            let error = try? JSONDecoder().decode(WireError.self, from: data)
            throw ClientError.response(http.statusCode, error?.message ?? HTTPURLResponse.localizedString(forStatusCode: http.statusCode))
        }
    }
}
