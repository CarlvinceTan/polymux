// @fixture --verify-fixture --verify-history
// A long chat opens at its newest message, and scrolling up loads the earlier messages.
const app = await cua.getApp(APP);
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "t-long" })).filter((item) => item.label.startsWith("t-long, "));
  if (!rows.length) await cua.sleep(500);
}
await app.click(rows[0].element_token);
// A bubble with actions reads as a button labelled with its text; plain text reads as static text.
const text = (item) => item.value || item.label || "";
const shown = async () => {
  for (let attempt = 0; attempt < 3; attempt++) {
    const state = await app.getState({ include_screenshot: false, timeout_ms: 5000, max_depth: 70,
      max_elements: 2000, tree_format: "elements" });
    if (state.truncated) {
      await cua.sleep(250);
      continue;
    }
    const window = state.elements.find(item => item.role === "AXWindow")?.frame;
    if (!window) throw new Error("History window is missing");
    return state.elements.filter(item => /^History message \d+$/.test(text(item)) && item.frame &&
      item.frame.y >= window.y && item.frame.y + item.frame.h <= window.y + window.h);
  }
  throw new Error("Incomplete history lookup");
};
const number = (item) => Number(text(item).split(" ")[2]);
let visible = [];
for (let i = 0; i < 20; i++) {
  visible = await shown();
  if (visible.some(item => number(item) === 150)) break;
  await cua.sleep(500);
}
if (!visible.length) throw new Error("the chat showed no messages");
if (!visible.some(item => number(item) === 150)) throw new Error("the chat did not open at its newest message");
if (Math.min(...visible.map(number)) <= 50) throw new Error("an earlier page loaded before scrolling");
// Each pass is three driver calls, keeping the flow inside cua-driver's 100-call budget.
for (let i = 0; i < 24; i++) {
  if (visible.length) {
    if (Math.min(...visible.map(number)) === 1) return "ok";
    await app.scroll(visible[Math.floor(visible.length / 2)].element_token, "up", 40);
  }
  // A scroll can land while the earlier page is still loading; let it settle before reading.
  await cua.sleep(300);
  visible = await shown();
}
throw new Error("scrolling up never reached the first message");
