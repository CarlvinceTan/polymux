#if DEBUG
import Foundation

enum CardCases {
    static var html: String {
        let arguments = CommandLine.arguments
        let ids = ["text", "forms", "choices", "data", "code", "charts", "diagram", "activity", "feedback", "empty", "long", "metrics", "advanced"]
        let selected = arguments.firstIndex(of: "--verify-card-view").flatMap { index in
            index + 1 < arguments.count ? arguments[index + 1] : nil
        } ?? "text"
        return source.replacingOccurrences(of: "__CASE__", with: ids.contains(selected) ? selected : "text")
    }

    private static let source = """
    <div class="card" id="caseCard">
      <header class="header"><div><h3 id="caseTitle">Reading</h3><small class="muted">Component preview</small></div><span class="badge" id="caseIndex">1 / 12</span></header>
      <section class="stack" data-case="text" data-title="Reading">
        <h2>A clear reading hierarchy</h2>
        <p>Longer answers need a comfortable rhythm, clear headings, and useful emphasis. This paragraph includes <strong>important information</strong>, <em>context</em>, and <code>inline code</code>.</p>
        <blockquote>“Make the next useful action clear.”<br><small class="muted">Project notes</small></blockquote>
        <ul><li>Content wraps naturally at narrow widths.</li><li>Lists keep their markers and indentation.</li></ul>
        <details><summary>More context</summary><p>Expanded content should keep the same spacing and remain readable inside a scrolling card.</p></details>
      </section>
      <section class="stack" data-case="forms" data-title="Forms" hidden>
        <div class="grid"><div class="field"><label for="caseName">Project</label><input id="caseName" name="project" value="Research workspace"></div><div class="field"><label for="caseEmail">Email</label><input id="caseEmail" name="email" type="email" placeholder="name@example.com"></div></div>
        <div class="grid"><div class="field"><label for="caseReport">Report</label><select id="caseReport" name="report"><option>Weekly</option><option>Monthly</option></select></div><div class="field"><label for="caseDate">Due</label><input id="caseDate" name="due" type="date" value="2026-11-02"></div></div>
        <div class="field"><label for="caseNotes">Notes</label><textarea id="caseNotes" name="notes" rows="2" placeholder="Add context for the report"></textarea><small>Visible labels stay separate from entry hints.</small></div>
        <div class="actions"><button class="primary" id="caseSave">Save</button><button disabled>Export</button><small class="muted" id="caseSaved" role="status">Draft</small></div>
      </section>
      <section class="stack" data-case="choices" data-title="Choices" hidden>
        <fieldset><legend>Delivery</legend><div class="list"><label class="item"><input name="delivery" type="radio" value="summary" checked><div>Summary<small>Key findings only</small></div></label><label class="item"><input name="delivery" type="radio" value="full"><div>Full report<small>Findings, evidence, and recommendations</small></div></label></div></fieldset>
        <div class="list"><label class="item"><input name="notify" type="checkbox" class="switch" checked><div>Notify<small>When the report is ready</small></div></label><label class="item"><input name="reviewed" type="checkbox"><div><span class="task">Review sources</span></div></label><label class="item"><input type="checkbox" disabled><div>Published<small>Unavailable in this example</small></div></label></div>
        <p class="muted" id="choiceStatus" role="status">Delivery: summary</p>
      </section>
      <section class="stack" data-case="data" data-title="Wide data" hidden>
        <div class="row between"><p class="muted">Quarterly comparison</p><button id="caseSort">Sort</button></div>
        <div class="table" tabindex="0" aria-label="Quarterly comparison"><table><thead><tr><th scope="col">Workspace</th><th scope="col" class="numeric">Forecast</th><th scope="col" class="numeric">Actual</th><th scope="col" class="numeric">Variance</th></tr></thead><tbody id="caseRows"><tr><td>Enterprise research</td><td class="numeric">$1,248,320.00</td><td class="numeric">$1,182,014.38</td><td class="numeric">−5.3%</td></tr><tr><td>Customer interviews</td><td class="numeric">$84,500.00</td><td class="numeric">$82,110.00</td><td class="numeric">−2.8%</td></tr><tr><td>Accessibility review</td><td class="numeric">$12,450.00</td><td class="numeric">$12,450.00</td><td class="numeric">0.0%</td></tr></tbody></table></div>
        <small class="muted" id="sortStatus" role="status">Original order</small>
        <dl class="facts"><div><dt>Period</dt><dd>Q4 2026</dd></div><div><dt>Currency</dt><dd>USD</dd></div></dl>
      </section>
      <section class="stack" data-case="code" data-title="Code and logs" hidden>
        <p>Use <kbd>⌘</kbd> + <kbd>Return</kbd> to apply <code>report.generate()</code>.</p>
        <pre tabindex="0" aria-label="Build output"><code>const result = await report.generate({ project: "Research workspace", includeSources: true });
      if (result.ready) {
        console.log("Report generated successfully.");
      }</code></pre>
        <details><summary>Execution details</summary><dl class="facts"><div><dt>Duration</dt><dd>2.4 s</dd></div><div><dt>Status</dt><dd>Complete</dd></div></dl></details>
        <p class="notice good" role="status">The example report is ready.</p>
      </section>
      <section class="stack" data-case="charts" data-title="Charts" hidden>
        <div class="grid"><div class="tile"><span class="label">Completed</span><span class="value" id="chartTotal">74</span></div><div class="tile"><span class="label">Target</span><span class="value">90</span></div></div>
        <div class="chart compact"><canvas id="caseChart" role="img" aria-label="Completed and planned work over five weeks">Completed: 12, 15, 14, 17, 16. Planned: 16, 18, 18, 19, 19.</canvas></div>
        <div class="actions"><button id="chartUpdate">Update</button><small class="muted" id="chartStatus" role="status">Sample data</small></div>
      </section>
      <section class="stack" data-case="diagram" data-title="Diagram" hidden>
        <svg width="280" height="154" viewBox="0 0 280 154" role="img" aria-label="Sources feed analysis, which produces a report">
          <path d="M140 39V63M140 98V120" stroke="var(--muted)" stroke-width="2"/>
          <path d="m135 57 5 6 5-6m-10 57 5 6 5-6" fill="none" stroke="var(--muted)" stroke-width="2"/>
          <g fill="var(--fill)"><rect x="30" y="3" width="220" height="36" rx="8"/><rect x="30" y="63" width="220" height="35" rx="8"/><rect x="30" y="120" width="220" height="32" rx="8"/></g>
          <g fill="var(--text)" font-family="system-ui" font-size="14" text-anchor="middle"><text x="140" y="26">Sources</text><text x="140" y="86">Analysis</text><text x="140" y="141">Report</text></g>
        </svg>
        <p class="muted">A responsive SVG using the same text and surface colours as the controls.</p>
        <details><summary>Text equivalent</summary><ol><li>Collect sources.</li><li>Analyse the evidence.</li><li>Write the report.</li></ol></details>
      </section>
      <section class="stack" data-case="activity" data-title="Activity" hidden>
        <div class="row between"><span class="loading" role="status">Example analysis</span><span class="badge info">Preview</span></div>
        <ol class="steps"><li data-state="done"><strong>Sources collected</strong><small>Complete · 8 documents</small></li><li data-state="active"><strong>Analysis</strong><small>In progress · demonstration</small></li><li><strong>Report</strong><small>Pending</small></li></ol>
        <div class="field"><label for="caseProgress">Completion</label><progress id="caseProgress" max="3" value="1"></progress></div>
        <div class="field"><label for="caseCapacity">Capacity</label><meter id="caseCapacity" min="0" max="100" low="25" high="80" optimum="50" value="65">65%</meter></div>
      </section>
      <section class="stack" data-case="feedback" data-title="Feedback" hidden>
        <p class="notice good" role="status">Saved successfully.</p><p class="notice warn">Two sources need review.</p><p class="notice info">Showing cached results.</p>
        <div class="field"><label for="caseInvalid">Email</label><input id="caseInvalid" value="research@" aria-invalid="true" aria-describedby="caseError"><small id="caseError" class="error">Enter a complete email address.</small></div>
        <p class="notice bad" role="alert">The example report couldn't be saved.</p>
        <div class="actions"><button class="danger" id="caseArchive">Archive</button><span class="muted" id="archiveStatus" role="status">Example item</span></div>
      </section>
      <section class="stack" data-case="empty" data-title="Empty state" hidden>
        <div class="panel"><div class="empty"><h3>No results</h3><div class="actions"><button id="caseCreate">Create</button></div></div></div>
        <p id="emptyStatus" class="muted" role="status" hidden>Example created</p>
      </section>
      <section class="stack" data-case="long" data-title="Long content" hidden>
        <div class="header"><div><h2>Customer research and accessibility findings for the international workspace</h2><p class="muted">A realistic title that needs several lines on a phone.</p></div><span class="badge warn">Waiting for review</span></div>
        <div class="list"><label class="item"><input name="longReview" type="checkbox"><div>Review the recommendations from every regional team<small>CustomerSuccessAccessibilityResearchWorkspace2026</small></div></label></div>
        <div class="actions"><button>Schedule</button><button>Download</button><button disabled>Synchronisation</button></div>
      </section>
      <section class="stack" data-case="metrics" data-title="Metrics" hidden>
        <div class="grid"><div class="tile"><span class="label">Revenue</span><span class="value">$12,480</span><span class="delta up">+12.4%</span></div><div class="tile"><span class="label">Orders</span><span class="value">248</span><span class="delta up">+8.2%</span></div><div class="tile"><span class="label">Refunds</span><span class="value">$420</span><span class="delta down">+2.1%</span></div></div>
        <dl class="facts"><div><dt>Average order</dt><dd>$50.32</dd></div><div><dt>Returning customers</dt><dd>63.7%</dd></div></dl>
        <div class="row"><span class="badge good">Healthy</span><span class="badge warn">Review</span><span class="badge bad">Failed</span><span class="badge info">Queued</span></div>
      </section>
      <section class="stack" data-case="advanced" data-title="Advanced" hidden>
        <div class="grid"><canvas id="advSketch" aria-label="Drawing pad" style="width:100%;height:120px;touch-action:none;background:var(--fill);border-radius:var(--radius)"></canvas><canvas id="advScene" aria-label="Spinning WebGL triangle" style="width:100%;height:120px"></canvas></div>
        <dl class="facts"><div><dt>Drawing</dt><dd id="advStrokes">0 strokes</dd></div><div><dt>WebGL</dt><dd id="advGL">Starting</dd></div><div><dt>Animation</dt><dd id="advFrames">Starting</dd></div><div><dt>Worker</dt><dd id="advWorker">Counting</dd></div><div><dt>WebAssembly</dt><dd id="advWasm">Loading</dd></div><div><dt>Expression</dt><dd id="advEval">Pending</dd></div><div><dt>Audio</dt><dd id="advAudio">Off</dd></div></dl>
        <div class="actions"><button id="advAudioStart">Audio</button><button id="advClear">Clear</button></div>
      </section>
      <div class="actions"><button id="caseBack">Back</button><button id="caseNext">Next</button><button id="caseInspect" aria-expanded="false">Inspect</button></div>
      <pre id="caseReportData" aria-label="Layout report" hidden></pre>
    </div>
    <script>
    const card=document.getElementById('caseCard'),sections=[...card.querySelectorAll('[data-case]')],saved=polymuxState||{},values=saved.values||{};
    let index=sections.findIndex(section=>section.dataset.case===(saved.current||'__CASE__'));if(index<0)index=0;let chart;
    const controls=[...card.querySelectorAll('input[name],select[name],textarea[name]')];
    controls.forEach(input=>{if(!(input.name in values))return;if(input.type==='checkbox')input.checked=values[input.name];else if(input.type==='radio')input.checked=input.value===values[input.name];else input.value=values[input.name];});
    const persist=()=>{const values={};controls.forEach(input=>{if(input.type==='radio'){if(input.checked)values[input.name]=input.value;}else values[input.name]=input.type==='checkbox'?input.checked:input.value;});polymuxSave({current:sections[index].dataset.case,values});};
    const choice=()=>{document.getElementById('choiceStatus').textContent='Delivery: '+card.querySelector('input[name=delivery]:checked').value;persist();};
    card.addEventListener('input',choice);card.addEventListener('change',choice);
    const inspect=()=>{const root=document.getElementById('polymux-card'),bounds=root.getBoundingClientRect(),active=sections[index],overflow=[],scroll=[];for(const node of active.querySelectorAll('*')){const rect=node.getBoundingClientRect();if(!rect.width||!rect.height)continue;const style=getComputedStyle(node);if(['auto','scroll'].includes(style.overflowX)&&node.scrollWidth>node.clientWidth+1)scroll.push(node.tagName.toLowerCase());if(rect.left>=bounds.left-1&&rect.right<=bounds.right+1)continue;let allowed=false;for(let parent=node.parentElement;parent&&parent!==root;parent=parent.parentElement){if(['auto','scroll'].includes(getComputedStyle(parent).overflowX)){allowed=true;break;}}if(!allowed)overflow.push(node.tagName.toLowerCase());}const framed=[...active.querySelectorAll('button,input:not([type=checkbox],[type=radio],[type=range]),select,textarea')].filter(node=>parseFloat(getComputedStyle(node).borderTopWidth)>0).length;document.getElementById('caseReportData').textContent=JSON.stringify({case:active.dataset.case,width:Math.round(bounds.width),overflow:overflow.length,scroll:scroll.join(',')||'none',frames:framed});};
    const draw=()=>{if(chart)return;chart=new Chart(document.getElementById('caseChart'),{type:'line',data:{labels:['Week 1','Week 2','Week 3','Week 4','Week 5'],datasets:[{label:'Completed',data:[12,15,14,17,16]},{label:'Planned',data:[16,18,18,19,19]}]},options:{scales:{y:{beginAtZero:true}}}});};
    const show=()=>{sections.forEach((section,i)=>section.hidden=i!==index);document.getElementById('caseTitle').textContent=sections[index].dataset.title;document.getElementById('caseIndex').textContent=(index+1)+' / '+sections.length;document.getElementById('caseBack').disabled=index===0;document.getElementById('caseNext').disabled=index===sections.length-1;document.getElementById('caseReportData').hidden=true;document.getElementById('caseInspect').setAttribute('aria-expanded','false');if(sections[index].dataset.case==='charts'){draw();chart.resize();}window.scrollTo(0,0);persist();};
    document.getElementById('caseBack').onclick=()=>{index--;show();};document.getElementById('caseNext').onclick=()=>{index++;show();};document.getElementById('caseInspect').onclick=()=>{const report=document.getElementById('caseReportData');report.hidden=!report.hidden;document.getElementById('caseInspect').setAttribute('aria-expanded',String(!report.hidden));if(!report.hidden)inspect();};
    document.getElementById('caseSave').onclick=()=>{document.getElementById('caseSaved').textContent='Saved '+document.getElementById('caseName').value;persist();};
    document.getElementById('caseSort').onclick=()=>{const rows=document.getElementById('caseRows');[...rows.children].sort((a,b)=>a.cells[0].textContent.localeCompare(b.cells[0].textContent)).forEach(row=>rows.append(row));document.getElementById('sortStatus').textContent='Sorted by workspace';};
    document.getElementById('chartUpdate').onclick=()=>{chart.data.datasets[0].data=[16,18,20,21,23];chart.update('none');document.getElementById('chartTotal').textContent='98';document.getElementById('chartStatus').textContent='Updated locally';};
    document.getElementById('caseArchive').onclick=()=>{document.getElementById('archiveStatus').textContent='Archived locally';};document.getElementById('caseCreate').onclick=()=>{document.getElementById('emptyStatus').hidden=false;};choice();show();
    </script>
    <script>
    (()=>{const $=id=>document.getElementById(id),section=card.querySelector('[data-case=advanced]'),rgba=name=>polymuxColor(name).match(/[\\d.]+/g).map(Number);
    const fit=canvas=>{const ratio=devicePixelRatio||1;canvas.width=Math.round(canvas.clientWidth*ratio);canvas.height=Math.round(canvas.clientHeight*ratio);};
    const sketch=$('advSketch'),pen=sketch.getContext('2d');let strokes=0,drawing=false;
    const point=event=>{const rect=sketch.getBoundingClientRect();return[(event.clientX-rect.left)*sketch.width/rect.width,(event.clientY-rect.top)*sketch.height/rect.height];};
    sketch.addEventListener('pointerdown',event=>{drawing=true;sketch.setPointerCapture(event.pointerId);pen.strokeStyle=polymuxColor('--text');pen.lineWidth=3*(devicePixelRatio||1);pen.lineCap='round';pen.beginPath();pen.moveTo(...point(event));});
    sketch.addEventListener('pointermove',event=>{if(!drawing)return;pen.lineTo(...point(event));pen.stroke();});
    sketch.addEventListener('pointerup',()=>{if(!drawing)return;drawing=false;$('advStrokes').textContent=++strokes+(strokes===1?' stroke':' strokes');});
    $('advClear').onclick=()=>{pen.clearRect(0,0,sketch.width,sketch.height);strokes=0;$('advStrokes').textContent='0 strokes';};
    try{const scene=$('advScene'),gl=scene.getContext('webgl');const shader=(type,source)=>{const s=gl.createShader(type);gl.shaderSource(s,source);gl.compileShader(s);return s;};const program=gl.createProgram();
    gl.attachShader(program,shader(gl.VERTEX_SHADER,'attribute vec2 p;uniform float t;void main(){float c=cos(t),s=sin(t);gl_Position=vec4(p.x*c-p.y*s,p.x*s+p.y*c,0,1);}'));
    gl.attachShader(program,shader(gl.FRAGMENT_SHADER,'precision mediump float;uniform vec4 k;void main(){gl_FragColor=k;}'));gl.linkProgram(program);gl.useProgram(program);
    gl.bindBuffer(gl.ARRAY_BUFFER,gl.createBuffer());gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([0,.8,-.7,-.5,.7,-.5]),gl.STATIC_DRAW);gl.enableVertexAttribArray(0);gl.vertexAttribPointer(0,2,gl.FLOAT,false,0,0);
    const time=gl.getUniformLocation(program,'t'),tint=gl.getUniformLocation(program,'k');const paint=()=>{const[r,g,b,a]=rgba('--series-1');gl.uniform4f(tint,r/255,g/255,b/255,a);};paint();addEventListener('polymux:theme',paint);
    let frames=0;const frame=now=>{requestAnimationFrame(frame);if(section.hidden)return;if(scene.width!==Math.round(scene.clientWidth*(devicePixelRatio||1)))fit(scene);gl.viewport(0,0,scene.width,scene.height);gl.clear(gl.COLOR_BUFFER_BIT);gl.uniform1f(time,now/600);gl.drawArrays(gl.TRIANGLES,0,3);if(++frames===30){$('advFrames').textContent='Running';$('advGL').textContent='Rendering';}};requestAnimationFrame(frame);
    }catch(error){$('advGL').textContent='Unavailable';}
    new ResizeObserver(()=>{if(!section.hidden&&sketch.width!==Math.round(sketch.clientWidth*(devicePixelRatio||1)))fit(sketch);}).observe(sketch);
    const worker=new Worker(URL.createObjectURL(new Blob(['let n=0;for(let i=2;i<100000;i++){let prime=true;for(let j=2;j*j<=i;j++)if(i%j===0){prime=false;break;}if(prime)n++;}postMessage(n);'],{type:'text/javascript'})));
    worker.onmessage=event=>{$('advWorker').textContent=event.data.toLocaleString('en-US')+' primes';worker.terminate();};
    WebAssembly.instantiate(Uint8Array.from(atob('AGFzbQEAAAABBwFgAn9/AX8DAgEABwcBA2FkZAAACgkBBwAgACABags='),c=>c.charCodeAt(0))).then(({instance})=>{$('advWasm').textContent='2 + 3 = '+instance.exports.add(2,3);},()=>{$('advWasm').textContent='Unavailable';});
    try{$('advEval').textContent='6 × 7 = '+new Function('return 6 * 7')();}catch(error){$('advEval').textContent='Unavailable';}
    let audio;$('advAudioStart').onclick=()=>{audio=audio||new AudioContext();audio.resume().then(()=>{$('advAudio').textContent='Audio '+audio.state;});};
    })();
    </script>
    """
}
#endif
