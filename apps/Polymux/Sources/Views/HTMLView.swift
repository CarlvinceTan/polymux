import SwiftUI
import WebKit
import PolymuxKit
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// What a card leaves behind when it scrolls away, so it comes back at its size and with its state.
@MainActor
private enum CardMemory {
    static var sizes: [URL: (height: CGFloat, width: CGFloat?)] = [:]
    static var states: [URL: String] = [:]
    static let sources = NSCache<NSURL, NSString>()
    static var assets: CardAssets?
}

struct HTMLView: View {
    let url: URL
    let name: String
    let client: APIClient
    var width: Double? = nil
    var height: Double? = nil
    /// Called with the text a card sends through `polymuxSend`, forwarded to the agent as a reply to the card.
    var action: ((String) -> Void)? = nil
    @Environment(\.colorScheme) private var scheme
    @State private var html: String?
    @State private var assets: CardAssets?
    @State private var measuredHeight: CGFloat = 160
    @State private var measuredWidth: CGFloat?
    @State private var issue: String?
    @State private var failed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HTMLWidth(width: fixedWidth, measured: measuredWidth) {
                Group {
                    if let html = html ?? CardMemory.sources.object(forKey: url as NSURL).map(String.init),
                       let assets = assets ?? CardMemory.assets {
                        NativeHTML(html: html, assets: assets, dark: scheme == .dark, requestedHeight: fixedHeight,
                                   hugWidth: fixedWidth == nil, state: CardMemory.states[url],
                                   measuredHeight: $measuredHeight, measuredWidth: $measuredWidth,
                                   action: action, save: { CardMemory.states[url] = $0 }, fail: { failed = true })
                            .frame(height: fixedHeight ?? measuredHeight)
                    } else if let issue {
                        Text(issue).font(.caption).foregroundStyle(.muted).padding(16)
                    } else {
                        ProgressView().frame(height: fixedHeight ?? CardMemory.sizes[url]?.height ?? 100)
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .background(html == nil ? AppBackground.color(scheme) : .clear)
            .clipShape(.rect(cornerRadius: BubbleStyle.radius))
            .accessibilityLabel(name)
            if failed {
                Text("Part of this card didn’t work.").font(.caption).foregroundStyle(.muted)
            }
        }
        .task(id: url) { await load() }
        .onChange(of: measuredHeight) { remember() }
        .onChange(of: measuredWidth) { remember() }
    }

    private var fixedHeight: CGFloat? {
        guard let height, height.isFinite else { return nil }
        return min(BubbleStyle.cardHeight, max(44, CGFloat(height)))
    }

    private var fixedWidth: CGFloat? {
        guard let width, width.isFinite else { return nil }
        return min(BubbleStyle.width, max(44, CGFloat(width)))
    }

    private func remember() {
        guard html != nil else { return }
        CardMemory.sizes[url] = (measuredHeight, measuredWidth)
    }

    private func load() async {
        html = CardMemory.sources.object(forKey: url as NSURL).map(String.init)
        measuredHeight = CardMemory.sizes[url]?.height ?? 160
        measuredWidth = CardMemory.sizes[url]?.width
        issue = nil
        failed = false
        do {
            let source = try await MediaCache.shared.text(url: url, name: name, client: client)
            let assets = try await CardAssets.pending.value
            try Task.checkCancellation()
            self.assets = assets
            CardMemory.assets = assets
            CardMemory.sources.totalCostLimit = 16 << 20
            CardMemory.sources.setObject(source as NSString, forKey: url as NSURL, cost: source.utf8.count)
            html = source
        } catch {
            guard !Task.isCancelled else { return }
            issue = "Couldn’t load this visual card."
        }
    }
}

private struct HTMLWidth: Layout {
    let width: CGFloat?
    let measured: CGFloat?

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        let available = proposal.width.flatMap { $0.isFinite ? max(0, $0) : nil } ?? BubbleStyle.width
        let desired = width ?? measured ?? BubbleStyle.width
        let target = min(available, min(BubbleStyle.width, max(44, desired)))
        let size = child.sizeThatFits(ProposedViewSize(width: target, height: proposal.height))
        return CGSize(width: target, height: size.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, anchor: .topLeading, proposal: ProposedViewSize(bounds.size))
    }
}

private struct CardAssets: Sendable {
    let styles: String
    let script: String
    let chart: String

    static let pending = Task.detached {
        try CardAssets(styles: resource("Card", "css"), script: resource("Card", "js"), chart: resource("Chart", "js"))
    }

    private static func resource(_ name: String, _ type: String) throws -> String {
        guard let url = Bundle.main.url(forResource: name, withExtension: type) else {
            throw CocoaError(.fileNoSuchFile)
        }
        return try String(contentsOf: url, encoding: .utf8)
    }
}

private struct CardInput {
    let html: String
    let assets: CardAssets
    let dark: Bool
    let requestedHeight: CGFloat?
    let hugWidth: Bool
    let state: String?
    let height: Binding<CGFloat>
    let width: Binding<CGFloat?>
    let action: ((String) -> Void)?
    let save: (String) -> Void
    let fail: () -> Void
}

#if os(macOS)
private struct NativeHTML: NSViewRepresentable {
    let html: String
    let assets: CardAssets
    let dark: Bool
    let requestedHeight: CGFloat?
    let hugWidth: Bool
    let state: String?
    @Binding var measuredHeight: CGFloat
    @Binding var measuredWidth: CGFloat?
    let action: ((String) -> Void)?
    let save: (String) -> Void
    let fail: () -> Void

    private var input: CardInput {
        CardInput(html: html, assets: assets, dark: dark, requestedHeight: requestedHeight, hugWidth: hugWidth, state: state,
                  height: $measuredHeight, width: $measuredWidth, action: action, save: save, fail: fail)
    }
    func makeCoordinator() -> HTMLHost { HTMLHost(input) }
    func makeNSView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateNSView(_ view: WKWebView, context: Context) { context.coordinator.update(view, input) }
    static func dismantleNSView(_ view: WKWebView, coordinator: HTMLHost) { coordinator.stop(view) }
}
#else
private struct NativeHTML: UIViewRepresentable {
    let html: String
    let assets: CardAssets
    let dark: Bool
    let requestedHeight: CGFloat?
    let hugWidth: Bool
    let state: String?
    @Binding var measuredHeight: CGFloat
    @Binding var measuredWidth: CGFloat?
    let action: ((String) -> Void)?
    let save: (String) -> Void
    let fail: () -> Void

    private var input: CardInput {
        CardInput(html: html, assets: assets, dark: dark, requestedHeight: requestedHeight, hugWidth: hugWidth, state: state,
                  height: $measuredHeight, width: $measuredWidth, action: action, save: save, fail: fail)
    }
    func makeCoordinator() -> HTMLHost { HTMLHost(input) }
    func makeUIView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateUIView(_ view: WKWebView, context: Context) { context.coordinator.update(view, input) }
    static func dismantleUIView(_ view: WKWebView, coordinator: HTMLHost) { coordinator.stop(view) }
}
#endif

@MainActor
private final class HTMLHost: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
    private var input: CardInput
    private var source: String?
    private var dark: Bool?
    private var hug = false
    private var contentHeight: CGFloat?
    private var failed = false
    private var reloads = 0
    private var saved: String?
    private var sentAt = -Double.infinity
    private weak var view: WKWebView?

    init(_ input: CardInput) {
        self.input = input
    }

    /// Text from a card's `polymuxSend`, capped so a runaway script cannot flood the agent.
    static func cardText(_ body: Any) -> String? {
        guard let text = (body as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) else { return nil }
        return text.isEmpty || text.utf8.count > 16 * 1024 ? nil : text
    }

    func make() -> WKWebView {
        let controller = WKUserContentController()
        controller.add(self, contentWorld: .defaultClient, name: "card")
        controller.add(self, contentWorld: .page, name: "cardState")
        controller.addUserScript(WKUserScript(source: Self.bridge, injectionTime: .atDocumentStart,
                                              forMainFrameOnly: true, in: .page))
        controller.addUserScript(WKUserScript(source: Self.isolate, injectionTime: .atDocumentStart,
                                              forMainFrameOnly: false))
        controller.addUserScript(WKUserScript(source: Self.watch, injectionTime: .atDocumentStart,
                                              forMainFrameOnly: true, in: .defaultClient))
        controller.addUserScript(WKUserScript(source: Self.measure, injectionTime: .atDocumentEnd,
                                              forMainFrameOnly: true, in: .defaultClient))
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.userContentController = controller
        configuration.preferences.javaScriptCanOpenWindowsAutomatically = false
        let view = WKWebView(frame: .zero, configuration: configuration)
        view.navigationDelegate = self
        view.uiDelegate = self
        #if os(macOS)
        view.underPageBackgroundColor = .clear
        #else
        view.isOpaque = false
        view.backgroundColor = .clear
        view.scrollView.backgroundColor = .clear
        view.scrollView.alwaysBounceVertical = false
        view.scrollView.isScrollEnabled = false
        #endif
        self.view = view
        return view
    }

    func update(_ view: WKWebView, _ input: CardInput) {
        self.input = input
        if let contentHeight { updateScroll(contentHeight) }
        if source != input.html {
            source = input.html
            contentHeight = nil
            failed = false
            reloads = 0
            saved = input.state
            dark = input.dark
            hug = input.hugWidth
            load(view)
        } else if dark != input.dark {
            dark = input.dark
            view.evaluateJavaScript(Self.retheme(dark: input.dark), in: nil, in: .page, completionHandler: nil)
        }
    }

    private func load(_ view: WKWebView) {
        view.loadHTMLString(Self.document(input.html, assets: input.assets, dark: input.dark, hug: hug, state: saved), baseURL: nil)
    }

    func stop(_ view: WKWebView) {
        view.stopLoading()
        let controller = view.configuration.userContentController
        controller.removeScriptMessageHandler(forName: "card", contentWorld: .defaultClient)
        controller.removeScriptMessageHandler(forName: "cardState", contentWorld: .page)
        controller.removeAllUserScripts()
        view.navigationDelegate = nil
        view.uiDelegate = nil
        self.view = nil
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.frameInfo.isMainFrame else { return }
        switch message.name {
        case "cardState":
            guard let text = message.body as? String, text.utf8.count <= 64 * 1024,
                  (try? JSONSerialization.jsonObject(with: Data(text.utf8), options: .fragmentsAllowed)) != nil else { return }
            saved = text
            input.save(text)
        default:
            guard let body = message.body as? [String: Any] else { return }
            if let text = body["send"] {
                send(text)
            } else if body["error"] != nil, !failed {
                failed = true
                input.fail()
            } else {
                resize(body)
            }
        }
    }

    private func send(_ body: Any) {
        let now = ProcessInfo.processInfo.systemUptime
        guard let text = Self.cardText(body), now - sentAt > 0.5 else { return }
        sentAt = now
        input.action?(text)
    }

    private func resize(_ size: [String: Any]) {
        if let number = size["height"] as? NSNumber {
            let value = CGFloat(number.doubleValue)
            if value.isFinite, value >= 0 {
                contentHeight = value
                let bounded = min(BubbleStyle.cardHeight, max(44, ceil(value)))
                if abs(input.height.wrappedValue - bounded) > 0.5 { input.height.wrappedValue = bounded }
                updateScroll(value)
            }
        }
        guard hug else { return }
        if size["fill"] as? Bool == true {
            if input.width.wrappedValue != nil { input.width.wrappedValue = nil }
            return
        }
        guard let number = size["width"] as? NSNumber else { return }
        let value = CGFloat(number.doubleValue)
        guard value.isFinite, value > 0 else { return }
        let bounded = min(BubbleStyle.width, max(44, ceil(value)))
        if input.width.wrappedValue != bounded { input.width.wrappedValue = bounded }
    }

    private func updateScroll(_ contentHeight: CGFloat) {
        #if os(iOS)
        view?.scrollView.isScrollEnabled = contentHeight > (input.requestedHeight ?? BubbleStyle.cardHeight)
        #endif
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping @MainActor (WKNavigationActionPolicy) -> Void) {
        let url = navigationAction.request.url
        let local = url?.absoluteString.split(separator: "#", maxSplits: 1).first == "about:blank"
            && navigationAction.targetFrame?.isMainFrame == true
        decisionHandler(local ? .allow : .cancel)
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        guard reloads == 0 else { input.fail(); return }
        reloads += 1
        load(webView)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        if (error as NSError).code != NSURLErrorCancelled { input.fail() }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) {
        if (error as NSError).code != NSURLErrorCancelled { input.fail() }
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping @MainActor () -> Void) { completionHandler() }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping @MainActor (Bool) -> Void) { completionHandler(false) }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String,
                 defaultText: String?, initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping @MainActor (String?) -> Void) { completionHandler(nil) }

    func webView(_ webView: WKWebView, requestMediaCapturePermissionFor origin: WKSecurityOrigin,
                 initiatedByFrame frame: WKFrameInfo, type: WKMediaCaptureType,
                 decisionHandler: @escaping @MainActor (WKPermissionDecision) -> Void) { decisionHandler(.deny) }

    private static func document(_ html: String, assets: CardAssets, dark: Bool, hug: Bool, state: String?) -> String {
        let chart = html.contains("Chart") ? assets.chart : ""
        let saved = (state ?? "null").replacingOccurrences(of: "<", with: "\\u003c")
        return """
        <!doctype html><html data-theme="\(dark ? "dark" : "light")" style="\(tokens(dark: dark))"><head>
        <meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; script-src 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval'; worker-src blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; form-action 'none'; base-uri 'none'">
        <style>\(assets.styles)</style>
        <script>window.polymuxState=\(saved);\n\(chart)\n\(assets.script)</script>
        </head><body><div id="polymux-card"\(hug ? " class=\"hug\"" : "")>\(html)</div></body></html>
        """
    }

    // Live theme switch: new tokens, then charts repaint without reloading the card.
    private static func retheme(dark: Bool) -> String {
        """
        document.documentElement.dataset.theme='\(dark ? "dark" : "light")';
        document.documentElement.setAttribute('style','\(tokens(dark: dark))');
        window.polymuxTheme&&window.polymuxTheme();
        """
    }

    // The window background the card sits on, and the app's accent pair, resolved for one appearance.
    private static func tokens(dark: Bool) -> String {
        let surface = hex(AppBackground.color(dark ? .dark : .light), dark: dark)
        let accent = hex(Color("AccentColor"), dark: dark)
        let onAccent = hex(Color("OnAccent"), dark: dark)
        return "color-scheme:\(dark ? "dark" : "light");--surface:\(surface);--accent:\(accent);--on-accent:\(onAccent)"
    }

    private static func hex(_ color: Color, dark: Bool) -> String {
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0
        #if os(macOS)
        NSAppearance(named: dark ? .darkAqua : .aqua)?.performAsCurrentDrawingAppearance {
            let resolved = NSColor(color).usingColorSpace(.sRGB) ?? .black
            resolved.getRed(&red, green: &green, blue: &blue, alpha: nil)
        }
        #else
        let traits = UITraitCollection(userInterfaceStyle: dark ? .dark : .light)
        traits.performAsCurrent {
            _ = UIColor(color).resolvedColor(with: traits).getRed(&red, green: &green, blue: &blue, alpha: nil)
        }
        #endif
        let channel = { (value: CGFloat) -> String in
            let byte = Int((min(max(value, 0), 1) * 255).rounded())
            let text = String(byte, radix: 16, uppercase: true)
            return byte < 16 ? "0" + text : text
        }
        return "#" + channel(red) + channel(green) + channel(blue)
    }

    // Cards reply by calling polymuxSend and keep state with polymuxSave.
    private static let bridge = """
    window.polymuxSend = (value) => {
      try {
        const text = typeof value === 'string' ? value : JSON.stringify(value);
        if (typeof text === 'string') window.dispatchEvent(new CustomEvent('polymux:send', {detail: text}));
      } catch (error) {}
    };
    window.polymuxSave = (value) => { try { window.webkit.messageHandlers.cardState.postMessage(JSON.stringify(value ?? null)); } catch (error) {} };
    """

    private static let isolate = """
    for(const name of ['RTCPeerConnection','webkitRTCPeerConnection']){Object.defineProperty(window,name,{value:undefined,writable:false,configurable:false});}
    """

    // Only the isolated world can turn a trusted interaction into a native send.
    private static let watch = """
    (() => {
      const post = (body) => { try { window.webkit.messageHandlers.card.postMessage(body); } catch (error) {} };
      let allowed = false;
      let reset;
      // A browser-made change (from a scripted checkbox click) is trusted but not a person; user activation is.
      for (const type of ['pointerup', 'click', 'keydown', 'change']) {
        window.addEventListener(type, (event) => {
          if (!event.isTrusted || event.repeat || navigator.userActivation?.isActive === false) return;
          allowed = true;
          clearTimeout(reset);
          reset = setTimeout(() => { allowed = false; }, 0);
        }, true);
      }
      window.addEventListener('polymux:send', (event) => {
        if (!allowed || typeof event.detail !== 'string') return;
        allowed = false;
        post({send: event.detail});
      }, true);
      window.addEventListener('error', (event) => {
        if (event instanceof ErrorEvent) post({error: true});
      }, true);
      window.addEventListener('unhandledrejection', () => post({error: true}));
    })();
    """

    // Reports must not depend on the window being visible: rAF is throttled in a
    // background window, so the first and delayed sends are unconditional.
    private static let measure = """
    (()=>{const send=()=>{const card=document.getElementById('polymux-card')||document.body;if(!card)return;const rect=card.getBoundingClientRect();window.webkit.messageHandlers.card.postMessage({height:Math.max(card.scrollHeight,rect.height),width:rect.width,fill:!!card.querySelector('.card,.grid,.chart,canvas,svg')});};let scheduled=false;const schedule=()=>{if(scheduled)return;scheduled=true;const run=()=>{scheduled=false;send();};if(document.visibilityState==='hidden'){setTimeout(run,16);}else{requestAnimationFrame(run);}};new ResizeObserver(schedule).observe(document.body);new MutationObserver(schedule).observe(document.documentElement,{childList:true,subtree:true,attributes:true,characterData:true});window.addEventListener('resize',schedule);document.addEventListener('load',schedule,true);send();for(const ms of[0,50,250,1000]){setTimeout(send,ms);}})();
    """
}
