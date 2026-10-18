import SwiftUI
import WebKit
#if os(macOS)
import AppKit
#else
import UIKit
#endif

struct HTMLView: View {
    let url: URL
    let name: String
    var width: Double? = nil
    var height: Double? = nil
    /// Called with the text a card sends through `polymuxSend`, forwarded to the agent as a user message.
    var action: ((String) -> Void)? = nil
    @Environment(\.colorScheme) private var scheme
    @State private var html: String?
    @State private var measuredHeight: CGFloat = 160
    @State private var measuredWidth: CGFloat?
    @State private var issue: String?

    var body: some View {
        HTMLWidth(width: fixedWidth, measured: measuredWidth) {
            Group {
                if let html {
                    NativeHTML(html: html, dark: scheme == .dark, requestedHeight: fixedHeight,
                               hugWidth: fixedWidth == nil, measuredHeight: $measuredHeight,
                               measuredWidth: $measuredWidth, action: action)
                        .frame(height: fixedHeight ?? measuredHeight)
                } else if let issue {
                    Text(issue).font(.caption).foregroundStyle(.secondary).padding(16)
                } else {
                    ProgressView().frame(height: 100)
                }
            }
            .frame(maxWidth: .infinity)
        }
        .background(html == nil ? AppBackground.color(scheme) : .clear)
        .clipShape(.rect(cornerRadius: BubbleStyle.radius))
        .accessibilityLabel(name)
        .task(id: url) { await load() }
    }

    private var fixedHeight: CGFloat? {
        guard let height, height.isFinite else { return nil }
        return min(BubbleStyle.cardHeight, max(44, CGFloat(height)))
    }

    private var fixedWidth: CGFloat? {
        guard let width, width.isFinite else { return nil }
        return min(BubbleStyle.width, max(44, CGFloat(width)))
    }

    private func load() async {
        html = nil
        measuredHeight = 160
        measuredWidth = nil
        issue = nil
        do {
            let file = try await MediaCache.shared.file(url: url, name: name)
            try Task.checkCancellation()
            let data = try Data(contentsOf: file)
            guard data.count <= 1024 * 1024, let source = String(data: data, encoding: .utf8) else {
                throw CocoaError(.fileReadCorruptFile)
            }
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

#if os(macOS)
private struct NativeHTML: NSViewRepresentable {
    let html: String
    let dark: Bool
    let requestedHeight: CGFloat?
    let hugWidth: Bool
    @Binding var measuredHeight: CGFloat
    @Binding var measuredWidth: CGFloat?
    let action: ((String) -> Void)?

    func makeCoordinator() -> HTMLHost {
        HTMLHost(height: $measuredHeight, width: $measuredWidth, action: action)
    }
    func makeNSView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateNSView(_ view: WKWebView, context: Context) {
        context.coordinator.update(view, html: html, dark: dark, requestedHeight: requestedHeight,
                                   hugWidth: hugWidth, height: $measuredHeight, width: $measuredWidth, action: action)
    }
    static func dismantleNSView(_ view: WKWebView, coordinator: HTMLHost) { coordinator.stop(view) }
}
#else
private struct NativeHTML: UIViewRepresentable {
    let html: String
    let dark: Bool
    let requestedHeight: CGFloat?
    let hugWidth: Bool
    @Binding var measuredHeight: CGFloat
    @Binding var measuredWidth: CGFloat?
    let action: ((String) -> Void)?

    func makeCoordinator() -> HTMLHost {
        HTMLHost(height: $measuredHeight, width: $measuredWidth, action: action)
    }
    func makeUIView(context: Context) -> WKWebView { context.coordinator.make() }
    func updateUIView(_ view: WKWebView, context: Context) {
        context.coordinator.update(view, html: html, dark: dark, requestedHeight: requestedHeight,
                                   hugWidth: hugWidth, height: $measuredHeight, width: $measuredWidth, action: action)
    }
    static func dismantleUIView(_ view: WKWebView, coordinator: HTMLHost) { coordinator.stop(view) }
}
#endif

@MainActor
private final class HTMLHost: NSObject, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
    private var height: Binding<CGFloat>
    private var width: Binding<CGFloat?>
    private var action: ((String) -> Void)?
    private var source: String?
    private var dark: Bool?
    private var requestedHeight: CGFloat?
    private var hugWidth = false
    private var contentHeight: CGFloat?
    private weak var view: WKWebView?

    init(height: Binding<CGFloat>, width: Binding<CGFloat?>, action: ((String) -> Void)?) {
        self.height = height
        self.width = width
        self.action = action
    }

    /// Text from a card's `polymuxSend`, capped so a runaway script cannot flood the agent.
    static func cardText(_ body: Any) -> String? {
        guard let text = (body as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) else { return nil }
        return text.isEmpty ? nil : String(text.prefix(1000))
    }

    func make() -> WKWebView {
        let controller = WKUserContentController()
        controller.add(self, contentWorld: .defaultClient, name: "cardSize")
        controller.add(self, contentWorld: .page, name: "cardAction")
        controller.addUserScript(WKUserScript(source: Self.bridge, injectionTime: .atDocumentStart,
                                              forMainFrameOnly: true, in: .page))
        controller.addUserScript(WKUserScript(source: Self.isolate, injectionTime: .atDocumentStart,
                                              forMainFrameOnly: false))
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

    func update(_ view: WKWebView, html: String, dark: Bool, requestedHeight: CGFloat? = nil,
                hugWidth: Bool, height: Binding<CGFloat>, width: Binding<CGFloat?>,
                action: ((String) -> Void)?) {
        self.height = height
        self.width = width
        self.action = action
        self.requestedHeight = requestedHeight
        self.hugWidth = hugWidth
        if let contentHeight { updateScroll(contentHeight) }
        let surface = Self.cardSurface(dark: dark)
        if source != html {
            source = html
            contentHeight = nil
            self.dark = dark
            view.loadHTMLString(Self.document(html, dark: dark, surface: surface, hug: hugWidth), baseURL: nil)
        } else if self.dark != dark {
            self.dark = dark
            view.evaluateJavaScript(Self.retheme(dark: dark, surface: surface),
                                    in: nil, in: .defaultClient, completionHandler: nil)
        }
    }

    func stop(_ view: WKWebView) {
        view.stopLoading()
        view.configuration.userContentController.removeScriptMessageHandler(forName: "cardSize", contentWorld: .defaultClient)
        view.configuration.userContentController.removeScriptMessageHandler(forName: "cardAction", contentWorld: .defaultClient)
        view.configuration.userContentController.removeAllUserScripts()
        view.navigationDelegate = nil
        view.uiDelegate = nil
        self.view = nil
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.name == "cardAction" {
            guard message.frameInfo.isMainFrame, let text = Self.cardText(message.body) else { return }
            action?(text)
            return
        }
        guard message.frameInfo.isMainFrame, let size = message.body as? [String: Any] else { return }
        if let number = size["height"] as? NSNumber {
            let value = CGFloat(number.doubleValue)
            if value.isFinite, value >= 0 {
                contentHeight = value
                let bounded = min(BubbleStyle.cardHeight, max(44, ceil(value)))
                if abs(height.wrappedValue - bounded) > 0.5 { height.wrappedValue = bounded }
                updateScroll(value)
            }
        }
        guard hugWidth, let number = size["width"] as? NSNumber else { return }
        let value = CGFloat(number.doubleValue)
        guard value.isFinite, value > 0 else { return }
        let bounded = min(BubbleStyle.width, max(44, ceil(value)))
        if width.wrappedValue != bounded { width.wrappedValue = bounded }
    }

    private func updateScroll(_ contentHeight: CGFloat) {
        #if os(iOS)
        view?.scrollView.isScrollEnabled = contentHeight > (requestedHeight ?? BubbleStyle.cardHeight)
        #endif
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping @MainActor (WKNavigationActionPolicy) -> Void) {
        let url = navigationAction.request.url
        let local = url?.absoluteString.split(separator: "#", maxSplits: 1).first == "about:blank"
            && navigationAction.targetFrame?.isMainFrame == true
        decisionHandler(local ? .allow : .cancel)
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

    private static func document(_ html: String, dark: Bool, surface: String, hug: Bool) -> String {
        """
        <!doctype html><html style="color-scheme:\(dark ? "dark" : "light")" data-lg-backdrop><head>
        <meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">
        <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; form-action 'none'; base-uri 'none'">
        <style>\(Self.styles(surface, hug: hug))</style>
        </head><body><div id="polymux-card">\(html)</div>
        <script>\(Self.library)\n\(Self.controls)</script></body></html>
        """
    }

    // Live theme switch: repaint the canvas the glass copies without reloading the card.
    private static func retheme(dark: Bool, surface: String) -> String {
        """
        document.documentElement.style.colorScheme='\(dark ? "dark" : "light")';
        document.documentElement.style.background='\(surface)';
        document.documentElement.style.setProperty('--lg-surface','\(surface)');
        """
    }

    // Opaque card canvas: the window background the card sits on, the surface the glass copies.
    private static func cardSurface(dark: Bool) -> String {
        let color = AppBackground.color(dark ? .dark : .light)
        var red: CGFloat = 0, green: CGFloat = 0, blue: CGFloat = 0
        #if os(macOS)
        NSAppearance(named: dark ? .darkAqua : .aqua)?.performAsCurrentDrawingAppearance {
            let resolved = NSColor(color).usingColorSpace(.sRGB) ?? .black
            _ = resolved.getRed(&red, green: &green, blue: &blue, alpha: nil)
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

    private static let library: String = {
        guard let url = Bundle.main.url(forResource: "LiquidGlass", withExtension: "js"),
              let source = try? String(contentsOf: url, encoding: .utf8) else { return "" }
        return source
    }()

    // Keep the glass surface intact when a button's label changes.
    private static let controls = """
    (()=>{const button=customElements.get('lg-button'),text=Object.getOwnPropertyDescriptor(Node.prototype,'textContent');if(!button||!text)return;Object.defineProperty(button.prototype,'textContent',{configurable:true,get(){return text.get.call(this.querySelector('.lg-button__label')||this);},set(value){text.set.call(this.querySelector('.lg-button__label')||this,value);}});})();
    """

    private static func styles(_ surface: String, hug: Bool) -> String {
        """
        html,body{margin:0;padding:0;overflow-x:hidden}
        html{background:\(surface);--lg-on-glass:CanvasText;--lg-text:CanvasText;--lg-text-muted:color-mix(in srgb,CanvasText 65%,transparent);--lg-surface:\(surface);--lg-track:light-dark(#e5e7eb,#34363b);--lg-track-off:light-dark(#d1d5db,#3c3e43);--lg-tint:light-dark(rgb(255 255 255 / .14),rgb(255 255 255 / .08));--lg-tint-strong:light-dark(rgb(255 255 255 / .5),rgb(32 34 38 / .5));--lg-indicator:light-dark(rgb(255 255 255 / .6),rgb(255 255 255 / .15));--lg-focus:Highlight}
        body{font:16px -apple-system,BlinkMacSystemFont,sans-serif;line-height:1.4;color:CanvasText;background:transparent}
        #polymux-card{display:flow-root;max-width:100%;overflow-wrap:anywhere}\(hug ? "\n#polymux-card{width:fit-content}" : "")
    *,*::before,*::after{box-sizing:border-box}
    :where(img,svg,video,canvas){max-width:100%}
    :where(h1,h2,h3,p){margin:0}
    :where(h1){font-size:1.5rem;font-weight:600;line-height:1.2}
    :where(h2){font-size:1.25rem;font-weight:600;line-height:1.25}
    :where(h3){font-size:1.1rem;font-weight:600}
    :where(button,input,select,textarea){font:inherit;color:inherit;max-width:100%}
    :where(button:not([class*="lg-"]),input:not([class*="lg-"]):not([type=checkbox]):not([type=radio]):not([type=range]):not([type=color]),select,textarea){padding:.55em .8em;border:1px solid color-mix(in srgb,CanvasText 14%,transparent);border-radius:.75em;background:color-mix(in srgb,Canvas 70%,transparent);backdrop-filter:blur(14px) saturate(1.4);-webkit-backdrop-filter:blur(14px) saturate(1.4);box-shadow:inset 0 1px 0 color-mix(in srgb,Canvas 60%,transparent);transition:background .15s,transform .15s}
    :where(button){cursor:pointer;white-space:normal}
    :where(button:hover:not(:disabled):not([class*="lg-"])){background:color-mix(in srgb,CanvasText 8%,Canvas 80%)}
    :where(button:active:not(:disabled):not([class*="lg-"])){transform:translateY(1px);background:color-mix(in srgb,CanvasText 14%,Canvas 80%)}
    :where(button,input,select,textarea):focus-visible{outline:2px solid Highlight;outline-offset:2px}
    :where(button,input,select,textarea):disabled{opacity:.45;cursor:not-allowed}
    :where(input[type=range],input[type=checkbox],input[type=radio]){accent-color:Highlight}
    :where(textarea){resize:vertical}
    :where(.row){display:flex;align-items:center;gap:12px;flex-wrap:wrap}
    :where(.row)>*{min-width:0}
    :where(.stack){display:flex;flex-direction:column;gap:12px}
    :where(.muted){color:color-mix(in srgb,CanvasText 65%,transparent)}
    /* WebKit lands the library's lens filter off-target; draw the glass material and skip the lens. */
    lg-button .lg-surface__glass,lg-panel .lg-surface__glass,.lg-refract{filter:none!important}
    lg-button .lg-surface__tint,lg-panel .lg-surface__tint{background-color:light-dark(rgb(15 23 42 / .06),rgb(255 255 255 / .05));background-image:linear-gradient(180deg,light-dark(rgb(255 255 255 / .55),rgb(255 255 255 / .12)),light-dark(rgb(255 255 255 / .25),rgb(255 255 255 / .03)));box-shadow:inset 0 1px 0 light-dark(rgb(255 255 255 / .95),rgb(255 255 255 / .38)),inset 0 -1px 1px light-dark(rgb(15 23 42 / .07),rgb(0 0 0 / .35)),inset 0 0 0 .5px light-dark(rgb(15 23 42 / .07),rgb(255 255 255 / .1))}
    lg-button .lg-surface__shadow,lg-panel .lg-surface__shadow{box-shadow:0 6px 16px light-dark(rgb(15 23 42 / .14),rgb(0 0 0 / .5)),0 1px 3px light-dark(rgb(15 23 42 / .1),rgb(0 0 0 / .35))}
    lg-slider .lg-thumb{box-shadow:var(--lg-shadow-1),0 0 0 .5px light-dark(rgb(15 23 42 / .08),rgb(255 255 255 / .1))}
    lg-switch .lg-switch__track{box-shadow:inset 0 1px 2px rgb(0 0 0 / .12)}
    lg-segmented .lg-lens-shadow{box-shadow:var(--lg-shadow-1),0 0 0 .5px light-dark(rgb(15 23 42 / .06),rgb(255 255 255 / .08))}
    @media(prefers-reduced-motion:reduce){:where(button,input,select,textarea){transition:none}:where(button:active:not(:disabled):not([class*="lg-"])){transform:none}}
    """
    }

    // Cards ask the agent by calling this; the app turns the text into the user's next message.
    private static let bridge = """
    window.polymuxSend = (value) => { try { window.webkit.messageHandlers.cardAction.postMessage(String(value)); } catch (error) {} };
    """

    private static let isolate = """
    for(const name of ['RTCPeerConnection','webkitRTCPeerConnection']){Object.defineProperty(window,name,{value:undefined,writable:false,configurable:false});}
    """

    // Reports must not depend on the window being visible: rAF is throttled in a
    // background window, so the first and delayed sends are unconditional.
    private static let measure = """
    (()=>{const send=()=>{const card=document.getElementById('polymux-card')||document.body;if(!card)return;const rect=card.getBoundingClientRect();window.webkit.messageHandlers.cardSize.postMessage({height:Math.max(card.scrollHeight,rect.height),width:rect.width});};let scheduled=false;const schedule=()=>{if(scheduled)return;scheduled=true;const run=()=>{scheduled=false;send();};if(document.visibilityState==='hidden'){setTimeout(run,16);}else{requestAnimationFrame(run);}};new ResizeObserver(schedule).observe(document.body);new MutationObserver(schedule).observe(document.documentElement,{childList:true,subtree:true,attributes:true,characterData:true});window.addEventListener('resize',schedule);document.addEventListener('load',schedule,true);send();for(const ms of[0,50,250,1000]){setTimeout(send,ms);}})();
    """
}
