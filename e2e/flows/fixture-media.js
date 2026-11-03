// @fixture --verify-fixture --verify-profile --verify-profile-media
// The media grid jumps to the exact attachment; opening it in the chat expands photos and videos.
let app = await cua.getApp(APP);
let rows = [];
for (let attempt = 0; attempt < 60 && !rows.length; attempt++) {
  const state = await app.getState({ query: "Chief", include_screenshot: false, timeout_ms: 5000, max_elements: 500, tree_format: "elements" });
  if (state.truncated) throw new Error("Incomplete chat lookup");
  rows = state.elements.filter(item => item.role === "AXButton" && item.label.startsWith("Chief, "));
  if (!rows.length) await cua.sleep(500);
}
if (!rows.length) throw new Error("Chief row is missing");
await app.click(rows[0].element_token);
await app.waitFor({ role: "button", name: "Edit Chief’s profile", timeout_ms: 10000 });
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.waitFor({ role: "button", name: "Media", timeout_ms: 10000 });
const host = await app.getState({ query: "Work", include_screenshot: false });
app = await cua.getApp({ pid: host.pid, windowId: host.window_id });
await app.click({ role: "button", name: "Media" });
const tiles = [];
for (let index = 1; index <= 6; index++) {
  const [tile] = await app.query({ role: "button", name: `Show Landscape ${index}.png in chat` });
  if (!tile?.frame) throw new Error("Photo thumbnail is missing: " + index);
  tiles.push(tile.frame);
}
if (!(tiles[0].x < tiles[1].x && tiles[1].x < tiles[2].x &&
      Math.abs(tiles[0].y - tiles[2].y) <= 1 && tiles[3].y > tiles[0].y &&
      Math.abs(tiles[3].y - tiles[5].y) <= 1)) throw new Error("Photos are not arranged in a grid");
async function reveal(name) {
  await app.click({ role: "button", name: `Show ${name} in chat` });
  const [header] = await app.query({ role: "button", name: "Edit Chief’s profile" });
  const [composer] = await app.query({ role: "textfield", name: "Message" });
  for (let attempt = 0; attempt < 20; attempt++) {
    const [media] = await app.query({ role: "button", name: `View ${name}` });
    const middle = media?.frame && media.frame.y + media.frame.h / 2;
    if (middle > header?.frame.y + header?.frame.h && middle < composer?.frame.y) return;
    await cua.sleep(100);
  }
  throw new Error("The media tile did not scroll its attachment into view: " + name);
}
async function expand(name) {
  await app.click({ role: "button", name: `View ${name}` });
  let window;
  for (let attempt = 0; attempt < 30; attempt++) {
    window = (await cua.listWindows({ pid: host.pid })).find(item => item.window_id !== host.window_id);
    if (window) break;
    await cua.sleep(100);
  }
  if (!window) throw new Error("The expanded viewer did not open: " + name);
  const viewer = await cua.getApp({ pid: host.pid, windowId: window.window_id });
  await viewer.waitFor({ text: name, timeout_ms: 5000 });
  await viewer.waitFor({ role: "button", name: "Open file" });
  await viewer.pressKey("escape");
  for (let attempt = 0; attempt < 20; attempt++) {
    if (!(await cua.listWindows({ pid: host.pid })).some(item => item.window_id === window.window_id)) return;
    await cua.sleep(100);
  }
  throw new Error("The expanded viewer did not close");
}
for (const name of ["Landscape 1.png", "Landscape 6.png", "Demo.mp4", "Outgoing.png", "Outgoing.mp4"]) {
  await reveal(name);
  await expand(name);
}
return "ok";
