// @fixture --verify-fixture --verify-load
// @cua
// Arrow keys keep the chosen result in view past the fold, and Return opens exactly that result.
const app = await cua.getApp(APP);
await cua.sleep(2500);
// These rows push the shell's buttons past a default read's element limit.
const button = async (name) => {
  for (let i = 0; i < 40; i++) {
    const { elements } = await app.getState({ query: name, include_screenshot: false, tree_format: "elements", max_elements: 1000, timeout_ms: 5000 });
    const match = elements.find(item => item.role === "AXButton" && item.label === name);
    if (match) return match;
    await cua.sleep(250);
  }
  throw new Error(name + " is missing");
};
await app.click((await button("Search")).element_token);
const field = { role: "textfield", name: "Search everything" };
await app.waitFor(field, { timeout_ms: 5000 });
await app.waitFor({ role: "button", name: "Close search" });
const results = async () => (await app.query({ role: "button" })).filter(item => /^Ready, /.test(item.label ?? ""));
for (let i = 0; i < 40 && (await results()).length < 6; i++) await cua.sleep(250);
const first = (await results())[0];
if (!first) throw new Error("No results listed");
if (!first.selected) throw new Error("Return's target isn't highlighted: " + JSON.stringify(first));
for (let i = 0; i < 12; i++) await app.pressKey("down", field);
await cua.sleep(400);
const panel = (await app.query({ role: "button", name: "Close search" }))[0].frame;
const chosen = (await results()).find(item => item.selected);
if (!chosen) throw new Error("Nothing chosen after the arrow keys");
if (chosen.frame.y < panel.y || chosen.frame.y + chosen.frame.h > panel.y + 600) throw new Error("Chosen result left the panel: " + JSON.stringify(chosen.frame));
await app.pressKey("return", field);
const name = chosen.label.split(", ")[1];
await button("Edit " + name + "’s profile");
await app.waitFor({ role: "button", name: "Close search", gone: true });
return "ok";
