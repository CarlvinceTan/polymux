// @fixture --verify-fixture --verify-profile --verify-profile-media
// The chat profile stays beside the chat; its previews expand without hiding the avatar editor.
const app = await cua.getApp(APP);
async function callInChat(name = "Chief") {
  for (let i = 0; i < 20; i++) {
    const [call] = await app.query({ role: "button", name: "Speech conversation" });
    const [dictation] = await app.query({ role: "button", name: "Start dictation" });
    const [header] = await app.query({ role: "button", name: `Edit ${name}’s profile` });
    if (call?.frame && dictation?.frame && header?.frame &&
        call.frame.x + call.frame.w / 2 <= dictation.frame.x + dictation.frame.w &&
        Math.abs(header.frame.y - call.frame.y) <= 2) return;
    await cua.sleep(50);
  }
  throw new Error("the chat header or call button is outside the chat toolbar");
}
async function profileRows(name) {
  const [dictation] = await app.query({ role: "button", name: "Start dictation" });
  const state = await app.getState({ query: name, include_screenshot: false, timeout_ms: 5000, max_elements: 500, tree_format: "elements" });
  if (state.truncated) throw new Error("Incomplete attachment lookup: " + name);
  return state.elements.filter(item => item.role === "AXButton" && item.label === name && item.frame.x >= dictation.frame.x + dictation.frame.w);
}
async function selectTab(name) {
  const state = await app.getState({ query: name, include_screenshot: false, timeout_ms: 5000, max_elements: 500, tree_format: "elements" });
  if (state.truncated) throw new Error("Incomplete tab lookup: " + name);
  const buttons = state.elements.filter(item => item.role === "AXButton" && item.label === name);
  if (buttons.length !== 1) throw new Error("Tab is missing or ambiguous: " + name);
  await app.click(buttons[0].element_token);
}
let rows = [];
for (let i = 0; i < 60 && !rows.length; i++) {
  rows = (await app.query({ role: "button", name: "Chief" })).filter((item) => item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ text: "Profile fixture reply", timeout_ms: 10000 });
await callInChat();
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Reviewing calendar", timeout_ms: 10000 });
const [work] = await app.query({ role: "button", name: "Work" });
if (!work?.selected) throw new Error("Work is not the default profile tab");
await callInChat();
await selectTab("Capabilities");
await app.waitFor({ text: "polymux", timeout_ms: 10000 });
await selectTab("Work");
await app.waitFor({ text: "Reviewing calendar" });
await app.waitFor({ text: "Finished task 5" });
if ((await app.query({ text: "Ordinary chat reply" })).length) throw new Error("Tasks includes an ordinary chat turn");
const state = await app.getState({ include_screenshot: false, timeout_ms: 5000, max_elements: 500 });
if (state.truncated) throw new Error("Tasks lookup is incomplete");
const heading = state.tree_markdown.search(/AX(?:StaticText = "Tasks"|Heading \(Tasks\))/);
if (heading < 0) throw new Error("Tasks section heading is missing");
const tree = state.tree_markdown.slice(heading);
if (!tree.includes("Reviewing calendar") || tree.indexOf("Reviewing calendar") > tree.indexOf("Finished task 5")) {
  throw new Error("active work is not first");
}
await app.click({ role: "button", name: "Edit avatar" });
await app.waitFor({ role: "button", name: "Close Chief’s avatar" });
await app.click({ role: "button", name: "Close Chief’s avatar" });
await app.waitFor({ text: "Reviewing calendar" });
await selectTab("Media");
await app.waitFor({ role: "button", name: "Show Landscape 1.png in chat" });
if ((await app.query({ text: "Shared media" })).length) throw new Error("Media heading is still shown");
for (const name of ["Landscape 1.png", "Landscape 6.png", "Demo.mp4", "Outgoing.png", "Outgoing.mp4"]) {
  if ((await profileRows(`Show ${name} in chat`)).length !== 1) throw new Error("media thumbnail is missing: " + name);
}
await selectTab("Files");
await app.waitFor({ role: "button", name: "View Plan.txt" });
await app.waitFor({ role: "button", name: "View Notes.md" });
if ((await app.query({ text: "Shared files" })).length) throw new Error("Files heading is still shown");
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "button", name: "Capabilities", gone: true });
await callInChat();
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ text: "Reviewing calendar" });
const [reopened] = await app.query({ role: "button", name: "Work" });
if (!reopened?.selected) throw new Error("Reopening the profile did not return to Work");
const [aria] = (await app.query({ role: "button", name: "Aria" })).filter((item) => item.label.startsWith("Aria, "));
await app.click(aria.element_token);
await app.waitFor({ role: "textfield", name: "Name", value: "Aria", timeout_ms: 10000 });
await callInChat("Aria");
await selectTab("Media");
await app.waitFor({ text: "No media yet." });
await selectTab("Files");
await app.waitFor({ text: "No files yet" });
return "ok";
