#if DEBUG
import Foundation
import CryptoKit
import PolymuxKit

final class CardFixture: @unchecked Sendable {
    static let shared = CardFixture()
    // Changed fixtures must not reuse a cached immutable attachment.
    private static let cardID = "fixture-card-" + SHA256.hash(data: Data(html.utf8)).prefix(8)
        .map { String(format: "%02x", $0) }.joined()
    private let lock = NSRecursiveLock()
    private var events: [Event] = []
    private var streams: [VerifyFixture] = []
    private var shown = false

    func handle(_ fixture: VerifyFixture) -> Bool {
        guard CommandLine.arguments.contains("--verify-cards"), let url = fixture.request.url else { return false }
        if url.path == "/attachments/\(Self.cardID)/content" {
            reply(fixture, data: Data(Self.html.utf8), type: "text/html")
            return true
        }
        guard url.path.hasPrefix("/threads/t-chief/") else { return false }
        return lock.withLock {
            if CommandLine.arguments.contains("--verify-card-preview"), !shown {
                shown = true
                append(Message(id: "card-reply", threadID: "t-chief", role: MessageRole.assistant,
                               parts: [Part(kind: PartKind.attachment, attachment: Attachment(
                                id: Self.cardID, kind: AttachmentKind.html, name: "Card kit.html", mimeType: "text/html",
                                size: Int64(Self.html.utf8.count), url: "/attachments/\(Self.cardID)/content", indexingState: "ready"))],
                               createdAt: WireDate.string(), status: MessageStatus.done, agentID: "t-chief"),
                       type: EventType.messageDone)
            }
            switch url.lastPathComponent {
            case "stream":
                let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                                               headerFields: ["Content-Type": "text/event-stream"])!
                fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
                let after = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
                    .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
                emit(events.filter { $0.seq > after }, to: fixture)
                streams.append(fixture)
            case "events":
                let after = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
                    .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
                reply(fixture, data: try! JSONEncoder().encode(events.filter { $0.seq > after }))
            case "messages" where fixture.request.httpMethod == "POST":
                do {
                    let send = try JSONDecoder().decode(SendRequest.self, from: body(fixture.request))
                    let date = WireDate.string()
                    let user = Message(id: send.id, threadID: "t-chief", role: MessageRole.user,
                                       parts: [Part(kind: PartKind.text, text: send.text)], createdAt: date,
                                       status: MessageStatus.done, replyToID: send.replyToID)
                    append(user, type: EventType.messageDone)
                    if !shown {
                        shown = true
                        var card = Message(id: "card-reply", threadID: "t-chief", role: MessageRole.assistant,
                                           parts: [], createdAt: date, status: MessageStatus.streaming, agentID: "t-chief")
                        append(card, type: EventType.messageStart)
                        card.parts = [Part(kind: PartKind.attachment, attachment: Attachment(
                            id: Self.cardID, kind: AttachmentKind.html, name: "Weekly focus.html", mimeType: "text/html",
                            size: Int64(Self.html.utf8.count), url: "/attachments/\(Self.cardID)/content", indexingState: "ready"))]
                        append(card, type: EventType.messageAttachment)
                        card.status = MessageStatus.done
                        append(card, type: EventType.messageDone)
                    } else {
                        let linked = send.replyToID == "card-reply" && send.replyPart == 0
                        let text = linked ? "Received your card reply." : "Unexpected card message."
                        append(Message(id: UUID().uuidString, threadID: "t-chief", role: MessageRole.assistant,
                                       parts: [Part(kind: PartKind.text, text: text)], createdAt: date,
                                       status: MessageStatus.done, agentID: "t-chief"), type: EventType.messageDone)
                    }
                    reply(fixture, data: Data(), status: 202)
                } catch { fixture.client?.urlProtocol(fixture, didFailWithError: error) }
            case "messages":
                var messages: [Message] = []
                for event in events {
                    guard let message = event.message else { continue }
                    if let index = messages.firstIndex(where: { $0.id == message.id }) { messages[index] = message }
                    else { messages.append(message) }
                }
                reply(fixture, data: try! JSONEncoder().encode(messages))
            default: return false
            }
            return true
        }
    }

    func remove(_ fixture: VerifyFixture) {
        lock.withLock { streams.removeAll { $0 === fixture } }
    }

    private func append(_ message: Message, type: String) {
        let seq = Int64(events.count + 1)
        var message = message
        message.seq = events.first { $0.messageID == message.id }?.message?.seq ?? seq
        let event = Event(seq: seq, conversationID: "t-chief", type: type, messageID: message.id,
                          message: message, createdAt: message.createdAt)
        events.append(event)
        for fixture in streams { emit([event], to: fixture) }
    }

    private func emit(_ events: [Event], to fixture: VerifyFixture) {
        let wire = events.map { "data: " + String(decoding: try! JSONEncoder().encode($0), as: UTF8.self) + "\n\n" }.joined()
        fixture.client?.urlProtocol(fixture, didLoad: Data((wire.isEmpty ? ": connected\n\n" : wire).utf8))
    }

    private func reply(_ fixture: VerifyFixture, data: Data, type: String = "application/json", status: Int = 200) {
        let response = HTTPURLResponse(url: fixture.request.url!, statusCode: status, httpVersion: nil,
                                       headerFields: ["Content-Type": type])!
        fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
        fixture.client?.urlProtocol(fixture, didLoad: data)
        fixture.client?.urlProtocolDidFinishLoading(fixture)
    }

    private func body(_ request: URLRequest) -> Data {
        if let data = request.httpBody { return data }
        guard let stream = request.httpBodyStream else { return Data() }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var bytes = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&bytes, maxLength: bytes.count)
            guard count > 0 else { break }
            data.append(contentsOf: bytes.prefix(count))
        }
        return data
    }

    private static var html: String {
        if CommandLine.arguments.contains("--verify-card-cases") { return CardCases.html }
        guard CommandLine.arguments.contains("--verify-card-kit") else { return planner }
        let arguments = CommandLine.arguments
        let view = arguments.firstIndex(of: "--verify-card-view").flatMap { index in
            index + 1 < arguments.count ? arguments[index + 1] : nil
        } ?? "overview"
        return kit.replacingOccurrences(of: "'__VIEW__'", with: "'\(["overview", "form", "activity", "states"].contains(view) ? view : "overview")'")
    }

    private static let kit = """
    <div class="card">
      <header class="header"><div><h3>Project workspace</h3><p class="muted">Built with the shared card kit</p></div><span class="badge info">Demo</span></header>
      <div id="examples" class="segmented" aria-label="Example" data-value="overview"><button value="overview">Overview</button><button value="form">Form</button><button value="activity">Activity</button><button value="states">States</button></div>
      <section class="stack" data-view="overview">
        <div class="grid"><div class="tile"><span class="label">Revenue</span><span class="value">$12,480</span><span class="delta up">+12.4%</span></div><div class="tile"><span class="label">Orders</span><span class="value">248</span><span class="delta up">+8.2%</span></div><div class="tile"><span class="label">Refunds</span><span class="value">$420</span><span class="delta down">+2.1%</span></div></div>
        <div class="table"><table><thead><tr><th scope="col">Channel</th><th scope="col" class="numeric">Orders</th><th scope="col" class="numeric">Revenue</th></tr></thead><tbody><tr><td>Direct</td><td class="numeric">164</td><td class="numeric">$8,320</td></tr><tr><td>Referrals</td><td class="numeric">84</td><td class="numeric">$4,160</td></tr></tbody></table></div>
        <p class="notice good" role="status">All channels are within budget.</p>
      </section>
      <section class="stack" data-view="form" hidden>
        <div class="grid"><div class="field"><label for="project">Project</label><input id="project" placeholder="Project name" aria-describedby="projectError"><small class="error" id="projectError" hidden>Enter a project name.</small></div><div class="field"><label for="cadence">Report</label><select id="cadence"><option>Weekly</option><option>Monthly</option></select></div></div>
        <div class="list"><label class="item"><input id="kitNotify" type="checkbox" class="switch" checked><div>Notify<small>When the report is ready</small></div></label></div>
        <div class="field"><label for="budget">Budget · <output id="budgetValue">$500</output></label><input id="budget" type="range" min="0" max="1000" value="500" step="50"></div>
        <div class="actions"><button class="primary" id="kitSave">Save</button><button id="fill">Fill</button><button disabled>Export</button><span class="muted" id="formStatus" role="status">Draft</span></div>
      </section>
      <section class="stack" data-view="activity" hidden>
        <div class="row between"><h3>Research preview</h3><span class="badge warn" id="runStatus">Example activity</span></div>
        <ol class="steps"><li data-state="done"><strong>Sources collected</strong><small>Complete · 8 documents</small></li><li id="analysisStep" data-state="active"><strong>Analysis</strong><small id="analysisStatus">In progress · demonstration</small></li><li><strong>Report</strong><small>Pending</small></li></ol>
        <progress id="kitProgress" max="3" value="1" aria-label="Example completion"></progress>
        <div class="actions"><button id="advance">Advance</button><button id="filter">Filter</button></div>
        <div id="filters" class="stack compact"></div>
      </section>
      <section class="stack" data-view="states" hidden>
        <div class="panel"><div class="empty"><h3>No results</h3><p class="muted">Project workspace</p></div></div>
        <div class="row between"><span class="loading" role="status">Loading example</span><span class="badge">Pending</span></div>
        <p class="notice bad" role="alert">The example report couldn't be saved.</p>
        <details><summary>Report details</summary><dl class="facts"><div><dt>Format</dt><dd>PDF</dd></div><div><dt>Size</dt><dd>2.4 MB</dd></div></dl></details>
      </section>
    </div>
    <script>
    const examples=document.getElementById('examples'),project=document.getElementById('project'),notify=document.getElementById('kitNotify'),budget=document.getElementById('budget'),cadence=document.getElementById('cadence');
    const state=polymuxState||{};
    examples.dataset.value=state.view||'__VIEW__';project.value=state.project||'';notify.checked=state.notify??true;budget.value=state.budget??500;cadence.value=state.cadence||'Weekly';
    const persist=()=>polymuxSave({view:examples.dataset.value,project:project.value,notify:notify.checked,budget:Number(budget.value),cadence:cadence.value});
    const show=()=>{document.querySelectorAll('[data-view]').forEach(view=>view.hidden=view.dataset.view!==examples.dataset.value);window.scrollTo(0,0);persist();};
    const amount=()=>{document.getElementById('budgetValue').textContent='$'+budget.value;persist();};
    examples.addEventListener('change',show);[project,notify,cadence].forEach(control=>control.addEventListener('input',persist));budget.addEventListener('input',amount);
    document.getElementById('kitSave').onclick=()=>{const invalid=!project.value.trim();project.setAttribute('aria-invalid',String(invalid));document.getElementById('projectError').hidden=!invalid;document.getElementById('formStatus').textContent=invalid?'Not saved':'Saved '+project.value;persist();};
    document.getElementById('fill').onclick=()=>{project.value='Quarterly research and customer feedback';persist();};
    document.getElementById('advance').onclick=()=>{document.getElementById('analysisStep').dataset.state='done';document.getElementById('analysisStatus').textContent='Complete · demonstration';document.getElementById('kitProgress').value=2;document.getElementById('runStatus').textContent='Analysis complete';};
    document.getElementById('filter').onclick=()=>{const filters=document.getElementById('filters');filters.innerHTML='<div class="segmented" aria-label="Activity filter" data-value="all"><button value="all">All</button><button value="queue" disabled>Queue</button><button value="done">Done</button></div><small id="filterStatus" class="muted">Filter: all</small>';filters.querySelector('.segmented').addEventListener('change',event=>{document.getElementById('filterStatus').textContent='Filter: '+event.detail.value;});};
    amount();show();
    </script>
    """

    private static let planner = """
    <div class="stack" style="padding:16px;gap:8px">
      <div class="row" style="justify-content:space-between"><h3>Weekly focus</h3><span id="themeStatus" class="badge">Planner</span></div>
      <div class="row grid"><div class="tile"><span class="label">Planned</span><span class="value" id="total">4 h</span><span class="delta up">+2 h this week</span></div><div class="tile"><span class="label">Complete</span><span class="value">72%</span><span class="muted">On track</span></div></div>
      <div class="chart" style="height:95px"><canvas id="trend" role="img" aria-label="Focus hours by day">Mon 2, Tue 4, Wed 3, Thu 5, Fri 4.</canvas></div>
      <div class="row" style="justify-content:space-between"><div id="range" class="segmented" aria-label="Range"><button value="day">Day</button><button value="week" aria-pressed="true">Week</button></div><label><input id="notify" type="checkbox" class="switch" checked>Notify</label></div>
      <input id="hours" aria-label="Hours" type="range" min="0" max="12" value="4">
      <p class="muted" id="status"></p>
      <div class="row"><button class="primary" id="send">Send</button><button disabled>Saved</button><button id="attempt">Attempt</button><button id="theme">Theme</button><button id="fail">Break</button></div>
    </div>
    <script>
    const hours=document.getElementById('hours'),total=document.getElementById('total'),range=document.getElementById('range'),notify=document.getElementById('notify'),status=document.getElementById('status');
    if(polymuxState){hours.value=polymuxState.hours;range.dataset.value=polymuxState.range;notify.checked=polymuxState.notify;}
    const refresh=()=>{const state={range:range.dataset.value,hours:Number(hours.value),notify:notify.checked};total.textContent=hours.value+' h';status.textContent=state.range+' · '+state.hours+' h · '+(state.notify?'notify':'quiet');polymuxSave(state);};
    hours.addEventListener('input',refresh);range.addEventListener('change',refresh);notify.addEventListener('change',refresh);document.addEventListener('DOMContentLoaded',refresh);
    const chart=new Chart(document.getElementById('trend'),{type:'bar',data:{labels:['Mon','Tue','Wed','Thu','Fri'],datasets:[{label:'Focus',data:[2,4,3,5,4]}]},options:{plugins:{legend:{display:false}},scales:{y:{beginAtZero:true,ticks:{stepSize:2}}}}});
    document.getElementById('send').onclick=()=>{polymuxSend({range:range.dataset.value,hours:Number(hours.value),notify:notify.checked});polymuxSend('Unsolicited');};
    document.getElementById('attempt').onclick=()=>{setTimeout(()=>{document.getElementById('send').click();},50);};
    document.getElementById('theme').onclick=()=>{
      const root=document.documentElement,dark=root.dataset.theme!=='dark';
      root.dataset.theme=dark?'dark':'light';root.style.colorScheme=root.dataset.theme;
      root.style.setProperty('--surface',dark?'#1e1e1e':'#fff');root.style.setProperty('--accent',dark?'#d1d1d1':'#383838');root.style.setProperty('--on-accent',dark?'#292929':'#fff');
      polymuxTheme();
      const colour=chart.getDatasetMeta(0).data[0].options.backgroundColor;
      if(colour!==getComputedStyle(root).getPropertyValue('--series-1').trim())throw new Error('Chart kept its old colour');
      document.getElementById('themeStatus').textContent='Theme: '+root.dataset.theme;
    };
    document.getElementById('fail').onclick=()=>{throw new Error('Fixture card error');};
    polymuxSend('Unsolicited');
    </script>
    """
}
#endif
