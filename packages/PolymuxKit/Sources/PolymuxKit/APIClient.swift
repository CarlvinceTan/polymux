import CryptoKit
import Foundation
#if os(macOS)
import Darwin
#endif

public struct APIClient: Sendable {
    public let baseURL: URL
    /// Differs for every client, so a re-paired server with the same address still counts as new.
    public let id = UUID()
    private let session: URLSession
    private let credential: Credential
    private let endpoint: @Sendable () async throws -> URL
    private let recover: @Sendable () async -> Void
    var token: String? { get async { await credential.value() } }

    public init(baseURL: URL, session: URLSession = .shared, token: String? = nil,
                endpoint: (@Sendable () async throws -> URL)? = nil,
                recover: @escaping @Sendable () async -> Void = {}) {
        self.init(baseURL: baseURL, session: session, token: token, discover: Self.discoverToken,
                  endpoint: endpoint, recover: recover)
    }

    init(baseURL: URL, session: URLSession, token: String?, discover: @escaping @Sendable (URL, URLSession) async -> String?,
         endpoint: (@Sendable () async throws -> URL)? = nil, recover: @escaping @Sendable () async -> Void = {}) {
        self.baseURL = baseURL
        self.session = session
        self.endpoint = endpoint ?? { baseURL }
        self.recover = recover
        credential = Credential(explicit: token) { await discover(baseURL, session) }
    }

    public func resolve(_ path: String) -> URL? {
        URL(string: path, relativeTo: baseURL)?.absoluteURL
    }

    public func vaultClient() -> VaultClient {
        VaultClient(baseURL: baseURL, authorization: { await credential.value() }, endpoint: endpoint)
    }

    public func agents() async throws -> [Agent] {
        try await get("agents")
    }

    public func network() async throws -> NetworkInfo {
        try await get("network")
    }

    public func privacy() async throws -> PrivacyStatus {
        try await get("privacy", retrying: false)
    }

    public func invite() async throws -> Invite {
        try await get("network/invite", retrying: false)
    }

    /// An invitation for another server to link with this one.
    public func peerInvite() async throws -> Invite {
        try await get("network/invite", query: [URLQueryItem(name: "role", value: "peer")], retrying: false)
    }

    public func linkPeer(_ invite: Invite) async throws {
        var request = try await request("network/peers")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(invite)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func movePreview(_ target: String, merge: Bool = false) async throws -> NetworkInfo.MovePreview {
        try await get("network/move", query: [URLQueryItem(name: "target", value: target), URLQueryItem(name: "merge", value: merge ? "1" : "0")], retrying: false)
    }

    public func startMove(_ target: String, merge: Bool = false) async throws {
        struct Body: Encodable { let target: String; let merge: Bool }
        var request = try await request("network/move")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Body(target: target, merge: merge))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    /// A credential of this device's own, for the app beside a server that's about to move away.
    public func ownDevice() async throws -> String {
        var request = try await request("network/move/own")
        request.httpMethod = "POST"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode([String: String].self, from: data)["token"] ?? ""
    }

    /// The credential in use, to save for a server that's moving.
    public func currentToken() async -> String? { await credential.value() }

    /// True while the server reports it is draining for an update; a failed check counts as no.
    public func serverUpdating() async -> Bool {
        struct Health: Decodable { let status: String }
        return (try? await get("health", retrying: false) as Health)?.status == "updating"
    }

    public func allowDevice(_ id: String) async throws {
        var request = try await request("network/devices/\(id)/allow")
        request.httpMethod = "POST"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func draft(_ threadID: String) async throws -> DraftState {
        try await get("threads/\(threadID)/draft")
    }

    public func saveDraft(_ threadID: String, _ value: DraftState) async throws -> DraftState {
        var request = try await request("threads/\(threadID)/draft")
        request.httpMethod = "PUT"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(value)
        let (data, response) = try await session.data(for: request)
        if (response as? HTTPURLResponse)?.statusCode == 409 {
            throw DraftConflict(current: try JSONDecoder().decode(DraftState.self, from: data))
        }
        try validate(response)
        return try JSONDecoder().decode(DraftState.self, from: data)
    }

    public func renameDevice(_ name: String, client: String) async throws {
        var request = try await request("network/devices/self")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["name": name, "client": client])
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    /// Names this server's network. With `first`, only if no device has named it yet.
    public func nameNetwork(_ name: String, first: Bool = false) async throws {
        struct Body: Encodable { let name: String; let first: Bool }
        var request = try await request("network")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Body(name: name, first: first))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func labelDevice(_ id: String, label: String) async throws {
        var request = try await request("network/devices/\(id)")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["label": label])
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

    func speechSocket() async throws -> URL {
        let base = try await endpoint()
        var parts = URLComponents(url: base.appendingPathComponent("speech/live"), resolvingAgainstBaseURL: false)
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

    public func deleteThread(id: String) async throws -> DeleteResponse {
        var request = try await request("threads/\(id)")
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

    /// The bot's workspace, tools and skills.
    public func configuration(agentID: String) async throws -> AgentConfiguration {
        try await get("agents/\(agentID)/configuration")
    }

    /// The MCP connections the bot can use.
    public func connections(agentID: String) async throws -> [MCPConnection] {
        try await get("agents/\(agentID)/mcp")
    }

    /// Whether each of the bot's MCP connections answers, probed on request.
    public func mcpStatus(agentID: String) async throws -> [MCPStatus] {
        try await get("agents/\(agentID)/mcp/status")
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

    public func answerChoice(threadID: String, messageID: String, optionID: String, text: String? = nil) async throws -> Message {
        try await post("threads/\(threadID)/messages/\(messageID)/answer", ChoiceAnswer(optionID: optionID, text: text))
    }

    public func react(threadID: String, messageID: String, request: ReactionRequest) async throws -> Message {
        try await write("threads/\(threadID)/messages/\(messageID)/reaction", method: "PUT", body: request)
    }

    public func answerCredential(threadID: String, messageID: String, value: String? = nil, skip: Bool = false) async throws -> Message {
        try await post("threads/\(threadID)/messages/\(messageID)/credential", CredentialAnswer(value: value, skip: skip))
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

    public func createThread(agentIDs: [String], title: String? = nil) async throws -> Thread {
        var request = try await request("threads")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(CreateThreadRequest(agentIDs: agentIDs, title: title))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Thread.self, from: data)
    }

    public func updateThread(id: String, title: String? = nil, agentIDs: [String]? = nil, pinned: Bool? = nil, pinOrder: Double? = nil) async throws -> Thread {
        var request = try await request("threads/\(id)")
        request.httpMethod = "PATCH"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(UpdateThreadRequest(agentIDs: agentIDs, title: title, pinned: pinned, pinOrder: pinOrder))
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(Thread.self, from: data)
    }

    public func messages(threadID: String, before: Int64? = nil) async throws -> [Message] {
        var query = [URLQueryItem(name: "limit", value: "100")]
        if let before { query.append(URLQueryItem(name: "before", value: String(before))) }
        return try await get("threads/\(threadID)/messages", query: query)
    }

    public func sidebar() async throws -> SidebarSnapshot {
        try await get("sidebar")
    }

    public func createSection(name: String) async throws -> Section {
        try await write("sections", method: "POST", body: SectionPatch(name: name))
    }

    public func updateSection(id: String, name: String? = nil, collapsed: Bool? = nil) async throws -> Section {
        try await write("sections/\(id)", method: "PATCH", body: SectionPatch(name: name, collapsed: collapsed))
    }

    public func providers() async throws -> [Provider] {
        try await get("providers")
    }

    public func orderProviders(_ ids: [String]) async throws -> [Provider] {
        try await write("providers/order", method: "PUT", body: ids)
    }

    public func providerOptions() async throws -> [ProviderOption] {
        try await get("providers/options")
    }

    public func startLogin(type: String, auth: String, provider: String? = nil) async throws -> ProviderLogin {
        var body = ["type": type, "auth": auth]
        if let provider { body["provider"] = provider }
        return try await write("providers/logins", method: "POST", body: body)
    }

    public func providerLogin(id: String, after: Int64) async throws -> ProviderLogin {
        var request = try await request("providers/logins/\(id)", query: [URLQueryItem(name: "after", value: String(after))])
        request.timeoutInterval = 35
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(ProviderLogin.self, from: data)
    }

    public func answerLogin(id: String, prompt: Int64, value: String) async throws {
        struct Answer: Encodable { let prompt: Int64; let value: String }
        try await post("providers/logins/\(id)/answer", body: Answer(prompt: prompt, value: value))
    }

    public func cancelLogin(id: String) async throws {
        var request = try await request("providers/logins/\(id)")
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func saveProvider(_ provider: Provider, isNew: Bool) async throws -> Provider {
        try await write(isNew ? "providers" : "providers/\(provider.id)", method: isNew ? "POST" : "PATCH", body: provider)
    }

    public func renameProvider(id: String, name: String) async throws -> Provider {
        try await write("providers/\(id)", method: "PATCH", body: ["name": name])
    }

    public func updateProvider(type: String, enabled: Bool? = nil, models: [String: Bool]? = nil) async throws -> [Provider] {
        struct Patch: Encodable { let enabled: Bool?; let models: [String: Bool]? }
        return try await write("providers/groups/\(type)", method: "PATCH", body: Patch(enabled: enabled, models: models))
    }

    public func setProviderKey(id: String, key: String) async throws {
        let _: [String: String] = try await write("providers/\(id)/key", method: "PUT", body: ["key": key])
    }

    /// Asks the provider for its models; the server replaces the provider's model list with them.
    public func discoverModels(id: String) async throws -> Provider {
        try await post("providers/\(id)/test")
    }

    public func deleteProvider(id: String) async throws {
        var request = try await request("providers/\(id)")
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    /// Deletes a section. Its chats return to the main list, or are deleted too when `chats` is set.
    public func deleteSection(id: String, chats: Bool) async throws {
        var request = try await request("sections/\(id)", query: chats ? [URLQueryItem(name: "chats", value: "1")] : [])
        request.httpMethod = "DELETE"
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    public func reorderSection(id: String, beforeID: String? = nil, afterID: String? = nil) async throws {
        try await post("sidebar/reorder", body: SectionOrder(kind: "sections", id: id, beforeID: beforeID, afterID: afterID))
    }

    public func placeThread(id: String, sectionID: String? = nil, pinned: Bool = false,
                            before: String? = nil, after: String? = nil) async throws -> Thread {
        try await write("threads/\(id)/placement", method: "PATCH",
                        body: ChatPlacement(sectionID: sectionID, pinned: pinned, beforeID: before, afterID: after))
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

    /// Searches everything, or one chat when `scope` is its ID.
    public func search(query: String, kind: String, scope: String? = nil) async throws -> [SearchResult] {
        try await get("search", query: [URLQueryItem(name: "q", value: query), URLQueryItem(name: "type", value: kind)]
            + (scope.map { [URLQueryItem(name: "thread", value: $0)] } ?? []))
    }

    public func search(threadID: String, query: String) async throws -> [SearchHit] {
        try await get("threads/\(threadID)/search", query: [URLQueryItem(name: "q", value: query)])
    }

    public func upload(file: URL, progress: @escaping @Sendable (Double) -> Void = { _ in }) async throws -> Attachment {
        let boundary = UUID().uuidString
        let name = file.lastPathComponent.replacingOccurrences(of: "\"", with: "")
            .replacingOccurrences(of: "\r", with: "").replacingOccurrences(of: "\n", with: "")
        let body = FileManager.default.temporaryDirectory.appending(path: "upload-\(boundary)")
        defer { try? FileManager.default.removeItem(at: body) }
        try Self.multipart(file: file, name: name, boundary: boundary, to: body)
        var request = try await request("attachments")
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        let (response, metadata) = try await session.upload(for: request, fromFile: body, delegate: UploadProgress(progress))
        try validate(metadata, data: response)
        return try JSONDecoder().decode(Attachment.self, from: response)
    }

    /// Writes the multipart body to disk in chunks, so a large file is never held in memory.
    private static func multipart(file: URL, name: String, boundary: String, to body: URL) throws {
        let input = try FileHandle(forReadingFrom: file)
        defer { try? input.close() }
        guard FileManager.default.createFile(atPath: body.path, contents: nil) else { throw CocoaError(.fileWriteUnknown) }
        let output = try FileHandle(forWritingTo: body)
        defer { try? output.close() }
        try output.write(contentsOf: Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"\(name)\"\r\nContent-Type: application/octet-stream\r\n\r\n".utf8))
        while let chunk = try input.read(upToCount: 1 << 20), !chunk.isEmpty {
            try output.write(contentsOf: chunk)
        }
        try output.write(contentsOf: Data("\r\n--\(boundary)--\r\n".utf8))
    }

    /// Tells the server this device's platform, timezone and allowed connections.
    public func reportClient(_ state: ClientState) async throws {
        let _: ClientState = try await write("client", method: "PUT", body: state)
    }

    /// Tool calls the server asks this device to run.
    public func clientRequests() -> AsyncThrowingStream<ClientRequest, any Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try await request("client/requests")
                    request.timeoutInterval = 45
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    let (bytes, response) = try await session.bytes(for: request)
                    try validate(response)
                    var parser = SSEParser<ClientRequest>()
                    for try await byte in bytes {
                        try Task.checkCancellation()
                        if let request = try parser.feed(byte) { continuation.yield(request) }
                    }
                    continuation.finish()
                } catch { continuation.finish(throwing: error) }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    public func answerClient(id: String, result: ClientResult) async throws {
        try await post("client/requests/\(id)", body: result)
    }

    public func stream(threadID: String, after: Int64) -> AsyncThrowingStream<Event, any Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try await request("threads/\(threadID)/stream", query: [URLQueryItem(name: "after", value: String(after))])
                    request.timeoutInterval = 45
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    let (bytes, response) = try await session.bytes(for: request)
                    try validate(response)
                    var parser = SSEParser<Event>()
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

    private func get<T: Decodable>(_ path: String, query: [URLQueryItem] = [], retrying: Bool = true) async throws -> T {
        for attempt in 0...2 {
            do {
                let (data, response) = try await session.data(for: request(path, query: query))
                try validate(response, data: data)
                return try JSONDecoder().decode(T.self, from: data)
            } catch {
                if retrying, attempt < 2, case ClientError.response(401, _) = error, await credential.renew() { continue }
                guard retrying, !Task.isCancelled, error.isTransient, attempt < 2 else { throw error }
                if error.isOffline { await recover() }
                try await Task.sleep(for: .milliseconds(250 * (attempt + 1)))
            }
        }
        throw ClientError.unavailable
    }

    private func post<T: Encodable>(_ path: String, body: T, query: [URLQueryItem] = []) async throws {
        var request = try await request(path, query: query)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
    }

    private func write<T: Decodable, Body: Encodable>(_ path: String, method: String, body: Body) async throws -> T {
        var request = try await request(path)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await session.data(for: request)
        try validate(response, data: data)
        return try JSONDecoder().decode(T.self, from: data)
    }

    /// Downloads a server file, such as an attachment, with this device's credential.
    /// Downloads a file from this server; other origins are refused, so attachment metadata can't make the app fetch elsewhere.
    public func download(_ url: URL) async throws -> (URL, URLResponse) {
        guard url.scheme == baseURL.scheme, url.host == baseURL.host, url.port == baseURL.port else { throw ClientError.invalidURL }
        let base = try await endpoint()
        guard var parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { throw ClientError.invalidURL }
        parts.scheme = base.scheme
        parts.host = base.host
        parts.port = base.port
        guard let routed = parts.url else { throw ClientError.invalidURL }
        var request = URLRequest(url: routed)
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        if let token = await credential.value() {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return try await session.download(for: request)
    }

    private func request(_ path: String, query: [URLQueryItem] = []) async throws -> URLRequest {
        let base = try await endpoint()
        var parts = URLComponents(url: base.appendingPathComponent(path), resolvingAgainstBaseURL: false)
        if !query.isEmpty { parts?.queryItems = query }
        guard let url = parts?.url else { throw ClientError.invalidURL }
        var request = URLRequest(url: url)
        request.timeoutInterval = 20
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        if let token = await credential.value() { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        try Task.checkCancellation()
        return request
    }

    private static func discoverToken(_ url: URL, session: URLSession) async -> String? {
        let paired = try? ServerCredential.load(url.absoluteString)?.token
        #if os(macOS)
        let host = url.host?.lowercased() ?? ""
        var name = [CChar](repeating: 0, count: Int(NI_MAXHOST))
        guard gethostname(&name, name.count) == 0 else { return paired }
        let local = text(name).lowercased().components(separatedBy: ".")[0]
        let localHost = ["localhost", "127.0.0.1", "::1", local, "\(local).local"].contains(host)
            || localAddresses().contains(host)
            || (host.hasSuffix(".ts.net") && !Set(Host(name: host).addresses).isDisjoint(with: localAddresses()))
        if localHost {
            let directory = ProcessInfo.processInfo.environment["POLYMUX_DATA_DIR"].map {
                URL(fileURLWithPath: $0, isDirectory: true)
            } ?? FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".polymux/data")
            if let token = try? String(contentsOf: directory.appendingPathComponent("devices/local-token"), encoding: .utf8),
               await proves(token, url: url, session: session) {
                return token
            }
        }
        #endif
        return paired
    }

    /// Whether the listener at `url` knows the owner token, so it is the real local server and
    /// not another process on the same port. Only an HMAC of a fresh nonce crosses the wire.
    static func proves(_ token: String, url: URL, session: URLSession) async -> Bool {
        let nonce = (0..<16).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
        var request = URLRequest(url: url.appending(path: "local/proof"))
        request.httpMethod = "POST"
        request.timeoutInterval = 3
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try? JSONEncoder().encode(["nonce": nonce])
        guard let (data, response) = try? await session.data(for: request),
              (response as? HTTPURLResponse)?.statusCode == 200,
              let proof = try? JSONDecoder().decode([String: String].self, from: data)["proof"] else { return false }
        let key = SymmetricKey(data: Data(token.trimmingCharacters(in: .whitespacesAndNewlines).utf8))
        let port = url.port ?? (url.scheme == "https" ? 443 : 80)
        let expected = HMAC<SHA256>.authenticationCode(for: Data("polymux-local-proof:\(port):\(nonce)".utf8), using: key)
            .map { String(format: "%02x", $0) }.joined()
        return proof == expected
    }

    #if os(macOS)
    /// Decodes a null-terminated C buffer.
    private static func text(_ buffer: [CChar]) -> String {
        String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
    }

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
                addresses.insert(text(host))
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

private struct SectionPatch: Encodable {
    var name: String? = nil
    var collapsed: Bool? = nil
}

private struct SectionOrder: Encodable {
    let kind: String
    let id: String
    let beforeID: String?
    let afterID: String?
}

private struct ChatPlacement: Encodable {
    let sectionID: String?
    let pinned: Bool
    let beforeID: String?
    let afterID: String?
    private enum CodingKeys: String, CodingKey { case pinned, sectionID, beforeID, afterID }

    func encode(to encoder: Encoder) throws {
        var fields = encoder.container(keyedBy: CodingKeys.self)
        try fields.encode(pinned, forKey: .pinned)
        if !pinned { try fields.encode(sectionID, forKey: .sectionID) }
        try fields.encodeIfPresent(beforeID, forKey: .beforeID)
        try fields.encodeIfPresent(afterID, forKey: .afterID)
    }
}

/// The bearer a client sends: given, or discovered once and kept. A failed discovery is
/// retried after a pause, so a local server that starts later is still found.
actor Credential {
    private let explicit: String?
    private let discover: @Sendable () async -> String?
    private var found: String?
    private var running: Task<String?, Never>?
    private var failedAt: ContinuousClock.Instant?

    init(explicit: String?, discover: @escaping @Sendable () async -> String?) {
        self.explicit = explicit
        self.discover = discover
        // Starts at once, off the main actor, so the first request rarely waits.
        if explicit == nil { running = Task.detached { await discover() } }
    }

    func value() async -> String? {
        if let explicit { return explicit }
        if let found { return found }
        if let failedAt, ContinuousClock.now - failedAt < .seconds(2) { return nil }
        let task = running ?? Task.detached { [discover] in await discover() }
        running = task
        let token = await task.value
        running = nil
        found = token
        failedAt = token == nil ? .now : nil
        return token
    }

    /// Forgets a discovered bearer the server refused. Reports whether a new one replaced it.
    func renew() async -> Bool {
        guard explicit == nil else { return false }
        let refused = found
        found = nil
        let token = await value()
        return token != nil && token != refused
    }
}

/// Reports an upload's sent fraction, at most once per percent.
private final class UploadProgress: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private let report: @Sendable (Double) -> Void
    // Touched only on the session's serial delegate queue.
    private var sent = 0

    init(_ report: @escaping @Sendable (Double) -> Void) { self.report = report }

    func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64,
                    totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
        guard totalBytesExpectedToSend > 0 else { return }
        let percent = Int(totalBytesSent * 100 / totalBytesExpectedToSend)
        guard percent > sent else { return }
        sent = percent
        report(Double(percent) / 100)
    }
}
