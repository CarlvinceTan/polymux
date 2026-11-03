import SwiftUI
import PolymuxKit
import WebKit

/// Mail is someone else's HTML: scripts never run, links open outside, and the only thing
/// it can load is an image over HTTPS. Tracking pixels load too, as in any mail app.
private let mailRules = #"""
[{"trigger":{"url-filter":"^https?://"},"action":{"type":"block"}},
 {"trigger":{"url-filter":"^https://","resource-type":["image"]},"action":{"type":"ignore-previous-rules"}}]
"""#

@MainActor private func mailRuleList() async -> WKContentRuleList? {
    await withCheckedContinuation { done in
        WKContentRuleListStore.default()?.compileContentRuleList(forIdentifier: "hub-mail-images", encodedContentRuleList: mailRules) { list, _ in
            done.resume(returning: list)
        } ?? done.resume(returning: nil)
    }
}

/// Answers the page's requests for a message's parts, with ranges so video and audio can play.
@MainActor private final class MailParts: NSObject, WKURLSchemeHandler {
    var load: (String) async -> Data? = { _ in nil }
    var kind: (String) -> String = { _ in "application/octet-stream" }
    private var active: Set<ObjectIdentifier> = []

    func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url else { return }
        let key = ObjectIdentifier(task)
        let range = task.request.value(forHTTPHeaderField: "Range")
        active.insert(key)
        Task {
            let data = await load(url.lastPathComponent)
            guard active.remove(key) != nil else { return }
            guard let data else {
                task.didFailWithError(URLError(.resourceUnavailable))
                return
            }
            let span = Self.span(range, count: data.count)
            var headers = ["Content-Type": kind(url.lastPathComponent), "Accept-Ranges": "bytes", "Content-Length": String(span.count)]
            if range != nil { headers["Content-Range"] = "bytes \(span.lowerBound)-\(max(span.upperBound - 1, 0))/\(data.count)" }
            if let response = HTTPURLResponse(url: url, statusCode: range == nil ? 200 : 206, httpVersion: "HTTP/1.1", headerFields: headers) {
                task.didReceive(response)
            }
            task.didReceive(data.subdata(in: span))
            task.didFinish()
        }
    }

    func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {
        active.remove(ObjectIdentifier(task))
    }

    private static func span(_ header: String?, count: Int) -> Range<Int> {
        guard let header, let spec = header.split(separator: "=").last else { return 0..<count }
        let ends = spec.split(separator: "-", omittingEmptySubsequences: false)
        let start = ends.first.flatMap { Int($0) } ?? 0
        let end = ends.count > 1 ? Int(ends[1]).map { $0 + 1 } ?? count : count
        return min(start, count)..<min(max(start, end), count)
    }
}

@MainActor final class MailCoordinator: NSObject, WKNavigationDelegate {
    fileprivate let parts = MailParts()
    var open: (String) -> Void = { _ in }
    var shown = ""

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        guard let url = action.request.url else { return .cancel }
        if url.scheme == HubMailPage.openScheme {
            open(url.lastPathComponent)
            return .cancel
        }
        guard action.navigationType == .linkActivated else { return action.navigationType == .other ? .allow : .cancel }
        if ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "") {
            #if os(macOS)
            NSWorkspace.shared.open(url)
            #else
            await UIApplication.shared.open(url)
            #endif
        }
        return .cancel
    }

    func show(_ html: String, in view: WKWebView) {
        guard html != shown else { return }
        shown = html
        Task {
            if let list = await mailRuleList() { view.configuration.userContentController.add(list) }
            view.loadHTMLString(html, baseURL: nil)
        }
    }
}

@MainActor private func mailView(_ coordinator: MailCoordinator) -> WKWebView {
    let configuration = WKWebViewConfiguration()
    configuration.defaultWebpagePreferences.allowsContentJavaScript = false
    configuration.websiteDataStore = .nonPersistent()
    configuration.setURLSchemeHandler(coordinator.parts, forURLScheme: HubMailPage.partScheme)
    let view = WKWebView(frame: .zero, configuration: configuration)
    view.navigationDelegate = coordinator
    view.clearsPageBackground()
    return view
}

extension WKWebView {
    /// Lets the panel behind show through a page that has no background of its own.
    func clearsPageBackground() {
        #if os(macOS)
        setValue(false, forKey: "drawsBackground")
        #else
        isOpaque = false
        backgroundColor = .clear
        scrollView.backgroundColor = .clear
        #endif
    }
}

#if os(macOS)
struct MailPage: NSViewRepresentable {
    let html: String
    var kind: (String) -> String = { _ in "application/octet-stream" }
    var load: (String) async -> Data? = { _ in nil }
    var open: (String) -> Void = { _ in }

    func makeCoordinator() -> MailCoordinator { MailCoordinator() }
    func makeNSView(context: Context) -> WKWebView { mailView(context.coordinator) }
    func updateNSView(_ view: WKWebView, context: Context) { update(view, context.coordinator) }
}
#else
struct MailPage: UIViewRepresentable {
    let html: String
    var kind: (String) -> String = { _ in "application/octet-stream" }
    var load: (String) async -> Data? = { _ in nil }
    var open: (String) -> Void = { _ in }

    func makeCoordinator() -> MailCoordinator { MailCoordinator() }
    func makeUIView(context: Context) -> WKWebView { mailView(context.coordinator) }
    func updateUIView(_ view: WKWebView, context: Context) { update(view, context.coordinator) }
}
#endif

extension MailPage {
    fileprivate func update(_ view: WKWebView, _ coordinator: MailCoordinator) {
        coordinator.parts.load = load
        coordinator.parts.kind = kind
        coordinator.open = open
        coordinator.show(html, in: view)
    }
}

/// The formatting at the caret, reported by the signature editor.
struct SignatureState: Equatable {
    var bold = false
    var italic = false
    var underline = false
    var alignment = "left"
}

private let editorScript = #"""
let saved = null;
function report() {
  const center = document.queryCommandState('justifyCenter'), right = document.queryCommandState('justifyRight');
  webkit.messageHandlers.editor.postMessage({html: document.body.innerHTML, text: document.body.innerText,
    bold: document.queryCommandState('bold'), italic: document.queryCommandState('italic'),
    underline: document.queryCommandState('underline'), alignment: center ? 'center' : right ? 'right' : 'left'});
}
function remember() {
  const selection = document.getSelection();
  if (selection && selection.rangeCount && document.body.contains(selection.anchorNode)) saved = selection.getRangeAt(0).cloneRange();
}
window.format = function (name, value) {
  document.body.focus();
  if (saved) { const selection = document.getSelection(); selection.removeAllRanges(); selection.addRange(saved); }
  document.execCommand('styleWithCSS', false, false);
  document.execCommand(name, false, value);
  report();
};
document.addEventListener('input', report);
document.addEventListener('selectionchange', () => { remember(); report(); });
"""#

@MainActor final class SignatureCoordinator: NSObject, WKScriptMessageHandler {
    var changed: (String, String) -> Void = { _, _ in }
    var state: (SignatureState) -> Void = { _ in }
    weak var view: WKWebView?

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        guard let body = message.body as? [String: Any] else { return }
        let text = (body["text"] as? String ?? "").replacingOccurrences(of: "\u{a0}", with: " ")
        changed(body["html"] as? String ?? "", text.hasSuffix("\n") ? String(text.dropLast()) : text)
        state(SignatureState(bold: body["bold"] as? Bool ?? false, italic: body["italic"] as? Bool ?? false,
                             underline: body["underline"] as? Bool ?? false, alignment: body["alignment"] as? String ?? "left"))
    }

    func format(_ name: String, _ value: String? = nil) {
        let argument = value.map { "'\($0.replacingOccurrences(of: "'", with: ""))'" } ?? "null"
        view?.evaluateJavaScript("window.format('\(name)', \(argument))", in: nil, in: .defaultClient) { _ in }
    }
}

@MainActor private func signatureView(_ coordinator: SignatureCoordinator, html: String, text: String, dark: Bool) -> WKWebView {
    let configuration = WKWebViewConfiguration()
    // The page's own scripts stay off; the editor's run in the app's own world.
    configuration.defaultWebpagePreferences.allowsContentJavaScript = false
    configuration.websiteDataStore = .nonPersistent()
    let controller = configuration.userContentController
    controller.addUserScript(WKUserScript(source: editorScript, injectionTime: .atDocumentEnd, forMainFrameOnly: true, in: .defaultClient))
    controller.add(coordinator, contentWorld: .defaultClient, name: "editor")
    let view = WKWebView(frame: .zero, configuration: configuration)
    view.clearsPageBackground()
    coordinator.view = view
    let color = dark ? "#f2f2f7" : "#1c1c1e"
    let source = html.isEmpty ? text.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;").replacingOccurrences(of: "\n", with: "<br>") : html
    view.loadHTMLString("<!doctype html><html><head><meta charset=\"utf-8\"><style>html{color-scheme:\(dark ? "dark" : "light")}"
        + "body{margin:0;padding:11px;min-height:150px;outline:0;color:\(color);font:12px Helvetica,Arial,sans-serif;line-height:1.55}p,div{margin:0}</style></head>"
        + "<body contenteditable=\"true\" role=\"textbox\" aria-multiline=\"true\" aria-label=\"Signature\" spellcheck=\"true\">\(source)</body></html>", baseURL: nil)
    return view
}

#if os(macOS)
private struct SignatureSurface: NSViewRepresentable {
    let html: String
    let text: String
    let coordinator: SignatureCoordinator
    @Environment(\.colorScheme) private var scheme

    func makeNSView(context: Context) -> WKWebView { signatureView(coordinator, html: html, text: text, dark: scheme == .dark) }
    func updateNSView(_ view: WKWebView, context: Context) {}
    static func dismantleNSView(_ view: WKWebView, coordinator: ()) {
        view.configuration.userContentController.removeAllScriptMessageHandlers()
    }
}
#else
private struct SignatureSurface: UIViewRepresentable {
    let html: String
    let text: String
    let coordinator: SignatureCoordinator
    @Environment(\.colorScheme) private var scheme

    func makeUIView(context: Context) -> WKWebView { signatureView(coordinator, html: html, text: text, dark: scheme == .dark) }
    func updateUIView(_ view: WKWebView, context: Context) {}
    static func dismantleUIView(_ view: WKWebView, coordinator: ()) {
        view.configuration.userContentController.removeAllScriptMessageHandlers()
    }
}
#endif

/// A signature's text with its toolbar. The server keeps only the formatting offered here.
struct SignatureEditor: View {
    let html: String
    let text: String
    let changed: (_ html: String, _ text: String) -> Void
    @State private var coordinator = SignatureCoordinator()
    @State private var state = SignatureState()
    @State private var font = "Helvetica"
    @State private var size = "3"
    @State private var color = Color.primary

    private static let fonts = ["Helvetica", "Arial", "Georgia", "Times New Roman", "Courier New"]
    private static let sizes: [(label: String, value: String)] = [("10", "1"), ("12", "2"), ("14", "3"), ("18", "4"), ("24", "5"), ("32", "6")]

    var body: some View {
        VStack(spacing: 0) {
            toolbar
            Divider()
            SignatureSurface(html: html, text: text, coordinator: coordinator).frame(height: 170)
        }
        .background(.primary.opacity(0.05), in: .rect(cornerRadius: PanelStyle.rowRadius))
        .onAppear {
            coordinator.changed = changed
            coordinator.state = { state = $0 }
        }
    }

    private var toolbar: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 2) {
                Picker("Font", selection: Binding(get: { font }, set: { font = $0; coordinator.format("fontName", $0) })) {
                    ForEach(Self.fonts, id: \.self) { Text($0).tag($0) }
                }
                .labelsHidden().fixedSize()
                Picker("Font size", selection: Binding(get: { size }, set: { size = $0; coordinator.format("fontSize", $0) })) {
                    ForEach(Self.sizes, id: \.value) { Text($0.label).tag($0.value) }
                }
                .labelsHidden().fixedSize()
                Divider().frame(height: 15).padding(.horizontal, 3)
                tool("Bold", symbol: "bold", on: state.bold) { coordinator.format("bold") }
                tool("Italic", symbol: "italic", on: state.italic) { coordinator.format("italic") }
                tool("Underline", symbol: "underline", on: state.underline) { coordinator.format("underline") }
                ColorPicker("Text colour", selection: Binding(get: { color }, set: { value in
                    color = value
                    coordinator.format("foreColor", value.hexString)
                }), supportsOpacity: false)
                .labelsHidden()
                Divider().frame(height: 15).padding(.horizontal, 3)
                tool("Align left", symbol: "text.alignleft", on: state.alignment == "left") { coordinator.format("justifyLeft") }
                tool("Align centre", symbol: "text.aligncenter", on: state.alignment == "center") { coordinator.format("justifyCenter") }
                tool("Align right", symbol: "text.alignright", on: state.alignment == "right") { coordinator.format("justifyRight") }
            }
            .padding(.horizontal, 5).padding(.vertical, 3)
        }
        .scrollIndicators(.hidden)
    }

    private func tool(_ title: String, symbol: String, on: Bool, action: @escaping () -> Void) -> some View {
        Button(title, systemImage: symbol, action: action)
            .labelStyle(.iconOnly).buttonStyle(.plain)
            .frame(width: 25, height: 25)
            .background(.primary.opacity(on ? PanelStyle.selectedTint : 0), in: .rect(cornerRadius: 6))
            .contentShape(.rect)
            .help(title)
            .accessibilityAddTraits(on ? .isSelected : [])
    }
}

private extension Color {
    var hexString: String {
        let resolved = resolve(in: EnvironmentValues())
        let part = { (value: Float) in String(format: "%02x", Int((max(0, min(1, value)) * 255).rounded())) }
        return "#" + part(resolved.red) + part(resolved.green) + part(resolved.blue)
    }
}
