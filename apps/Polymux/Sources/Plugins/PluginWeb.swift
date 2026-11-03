import SwiftUI
import WebKit
import PolymuxKit

/// A web plugin's own page, which fills the side panel.
struct PluginWeb: View {
    let plugin: Plugin
    let chat: String
    let client: APIClient
    let close: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL
    @State private var failed = false

    var body: some View {
        if failed {
            Text("Couldn’t open \(plugin.title).").foregroundStyle(.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            PluginWebView(input: PluginWebInput(name: plugin.name, page: plugin.page ?? "index.html", chat: chat, client: client,
                                                dark: scheme == .dark, close: close, open: { openURL($0) },
                                                fail: { failed = true }))
                .accessibilityLabel(plugin.title)
        }
    }
}

private struct PluginWebInput {
    let name: String
    let page: String
    let chat: String
    let client: APIClient
    let dark: Bool
    let close: () -> Void
    let open: (URL) -> Void
    let fail: () -> Void

    var url: URL? {
        var parts = URLComponents()
        parts.scheme = PluginFiles.scheme
        parts.host = name
        parts.path = "/" + page
        parts.queryItems = [URLQueryItem(name: "chat", value: chat), URLQueryItem(name: "theme", value: dark ? "dark" : "light")]
        return parts.url
    }
}

#if os(macOS)
private struct PluginWebView: NSViewRepresentable {
    let input: PluginWebInput

    func makeCoordinator() -> PluginWebHost { PluginWebHost(input) }
    func makeNSView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateNSView(_ view: WKWebView, context: Context) { context.coordinator.update(view, input) }
    static func dismantleNSView(_ view: WKWebView, coordinator: PluginWebHost) { coordinator.stop(view) }
}
#else
private struct PluginWebView: UIViewRepresentable {
    let input: PluginWebInput

    func makeCoordinator() -> PluginWebHost { PluginWebHost(input) }
    func makeUIView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateUIView(_ view: WKWebView, context: Context) { context.coordinator.update(view, input) }
    static func dismantleUIView(_ view: WKWebView, coordinator: PluginWebHost) { coordinator.stop(view) }
}
#endif

/// Answers a page's requests through the app's client, so the page never holds the device's
/// credential and can only reach its own plugin.
@MainActor
private final class PluginFiles: NSObject, WKURLSchemeHandler {
    nonisolated static let scheme = "polymux-plugin"
    private let name: String
    private let client: APIClient
    private var running: [ObjectIdentifier: Task<Void, Never>] = [:]

    init(name: String, client: APIClient) {
        self.name = name
        self.client = client
    }

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url, url.host == name else {
            task.didFailWithError(URLError(.resourceUnavailable))
            return
        }
        let id = ObjectIdentifier(task)
        let path = String(url.path.dropFirst())
        let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let method = task.request.httpMethod ?? "GET"
        let body = Self.body(task.request)
        let type = task.request.value(forHTTPHeaderField: "Content-Type")
        running[id] = Task {
            do {
                let (data, reply) = try await client.pluginWeb(name, path: path, query: query, method: method, body: body, contentType: type)
                guard running[id] != nil else { return }
                let fields = (reply.value(forHTTPHeaderField: "Content-Type")).map { ["Content-Type": $0] }
                guard let response = HTTPURLResponse(url: url, statusCode: reply.statusCode, httpVersion: "HTTP/1.1", headerFields: fields) else {
                    throw URLError(.badServerResponse)
                }
                task.didReceive(response)
                task.didReceive(data)
                task.didFinish()
            } catch {
                guard running[id] != nil else { return }
                task.didFailWithError(error)
            }
            running[id] = nil
        }
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {
        running.removeValue(forKey: ObjectIdentifier(task))?.cancel()
    }

    func cancel() {
        running.values.forEach { $0.cancel() }
        running = [:]
    }

    private static func body(_ request: URLRequest) -> Data? {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 64 * 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data
    }
}

@MainActor
private final class PluginWebHost: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
    private var input: PluginWebInput
    private var dark: Bool
    private let files: PluginFiles

    init(_ input: PluginWebInput) {
        self.input = input
        dark = input.dark
        files = PluginFiles(name: input.name, client: input.client)
    }

    func make() -> WKWebView {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(files, forURLScheme: PluginFiles.scheme)
        configuration.userContentController.add(self, name: "polymux")
        configuration.userContentController.addUserScript(WKUserScript(source: Self.bridge(chat: input.chat, dark: dark),
                                                                       injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = self
        view.uiDelegate = self
        if let url = input.url { view.load(URLRequest(url: url)) }
        return view
    }

    func update(_ view: WKWebView, _ input: PluginWebInput) {
        self.input = input
        guard dark != input.dark else { return }
        dark = input.dark
        view.evaluateJavaScript(Self.theme(dark: dark) + "window.dispatchEvent(new Event('polymux:theme'));", in: nil, in: .page, completionHandler: nil)
    }

    func stop(_ view: WKWebView) {
        view.stopLoading()
        files.cancel()
        view.configuration.userContentController.removeScriptMessageHandler(forName: "polymux")
        view.navigationDelegate = nil
        view.uiDelegate = nil
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.frameInfo.isMainFrame, message.body as? String == "close" { input.close() }
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping @MainActor (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url else { return decisionHandler(.cancel) }
        let own = url.scheme == PluginFiles.scheme && url.host == input.name
        if own || navigationAction.targetFrame?.isMainFrame == false || ["about", "blob", "data"].contains(url.scheme ?? "") {
            decisionHandler(.allow)
        } else {
            decisionHandler(.cancel)
            external(url)
        }
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration, for navigationAction: WKNavigationAction,
                 windowFeatures: WKWindowFeatures) -> WKWebView? {
        navigationAction.request.url.map(external)
        return nil
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) {
        if (error as NSError).code != NSURLErrorCancelled { input.fail() }
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationResponse: WKNavigationResponse,
                 decisionHandler: @escaping @MainActor (WKNavigationResponsePolicy) -> Void) {
        let status = (navigationResponse.response as? HTTPURLResponse)?.statusCode ?? 200
        if navigationResponse.isForMainFrame, status >= 400 {
            decisionHandler(.cancel)
            input.fail()
        } else {
            decisionHandler(.allow)
        }
    }

    private func external(_ url: URL) {
        if url.scheme == "http" || url.scheme == "https" { input.open(url) }
    }

    private static func bridge(chat: String, dark: Bool) -> String {
        let id = (try? JSONEncoder().encode(chat)).map { String(decoding: $0, as: UTF8.self) } ?? "\"\""
        return """
        window.polymux = Object.freeze({chat: \(id), close() { window.webkit.messageHandlers.polymux.postMessage('close'); }});
        \(theme(dark: dark))
        """
    }

    private static func theme(dark: Bool) -> String {
        let name = dark ? "dark" : "light"
        return "document.documentElement.dataset.theme='\(name)';document.documentElement.style.colorScheme='\(name)';"
    }
}
