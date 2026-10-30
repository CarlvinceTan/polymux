// @fixture --verify-fixture --verify-tools -lastCreation.http://127.0.0.1:7337 bot
// A New Bot form open at a crash comes back with what was typed. The launch argument opens the form, since flows cannot drive its menu.
const app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "Name", timeout_ms: 10000 });
const [shapes] = await app.query({ role: "button", name: "Shapes" });
if (!shapes?.selected) throw new Error("New Bot did not start on Shapes");
// A name enables Create, whose frame bounds the picker below.
await app.typeText("Crash Bot", { role: "textfield", name: "Name" });
const categories = ["Shapes", "Food", "Animals", "Nature", "Objects"];
const colors = ["Black / White", "Brown", "Red", "Orange", "Amber", "Green", "Turquoise", "Blue", "Purple", "Pink", "Grey"];
const counts = { Shapes: 25, Food: 24, Animals: 24, Nature: 23, Objects: 25 };
for (const category of categories) {
  await app.click({ role: "button", name: category });
  const state = await app.getState({ include_screenshot: false, tree_format: "elements", max_elements: 250 });
  if (state.truncated) throw new Error("Picker lookup is incomplete");
  const window = state.elements.find(item => item.role === "AXWindow")?.frame;
  if (!window) throw new Error("New Bot window is missing");
  // macOS text predictions can appear as buttons outside the app window.
  const buttons = state.elements.filter(item => item.role === "AXButton" && item.frame &&
    item.frame.x >= window.x && item.frame.x + item.frame.w <= window.x + window.w &&
    item.frame.y >= window.y && item.frame.y + item.frame.h <= window.y + window.h);
  const tabs = buttons.filter(item => categories.includes(item.label));
  const choices = buttons.filter(item => !categories.includes(item.label) && !["Create", "Close New Bot", "Dismiss panel"].includes(item.label) && item.label);
  const create = buttons.find(item => item.label === "Create");
  const name = state.elements.find(item => item.label === "Name" && item.role === "AXTextField");
  if (choices.length !== counts[category]) throw new Error(category + " has " + choices.length + " choices; expected " + counts[category]);
  for (const choice of choices) {
    if (!choice.frame || choice.frame.y < name.frame.y + name.frame.h || choice.frame.y + choice.frame.h > create.frame.y - 16) {
      throw new Error(category + ": " + choice.label + " requires scrolling or overlaps the actions");
    }
  }
  const left = Math.min(...tabs.map(item => item.frame.x));
  const right = Math.max(...tabs.map(item => item.frame.x + item.frame.w));
  if (Math.abs((left + right) / 2 - (name.frame.x + name.frame.w / 2)) > 1) throw new Error("Categories are not centred");
  if (category === "Shapes") {
    for (const color of colors) if (!choices.some(item => item.label === color)) throw new Error(color + " is missing");
  }
}
async function choose(name) {
  await app.click({ role: "button", name });
  await app.waitFor({ role: "button", name, value: "Selected", timeout_ms: 1000 });
  const preview = await app.getState({ query: "avatar", include_screenshot: false, tree_format: "markdown" });
  if (!preview.tree_markdown.includes(`(${name} avatar, Idle)`)) throw new Error("Preview did not change to " + name);
}
for (const [category, names] of [["Animals", ["Panda", "Crab", "Elephant"]], ["Food", ["Cupcake", "Hamburger", "Popcorn"]], ["Nature", ["Palm", "Snowman", "Lightning"]], ["Objects", ["Light bulb", "Hammer", "Watering can"]]]) {
  await app.click({ role: "button", name: category });
  for (const name of names) await choose(name);
}
await app.click({ role: "button", name: "Shapes" });
for (const name of ["Square", "Hexagon", "Circle"]) await choose(name);
await app.click({ role: "button", name: "Red" });
await cua.sleep(800);
return "ok";
// @crash
const app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "Name", value: "Crash Bot", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Circle", value: "Selected" });
await app.waitFor({ role: "button", name: "Red", value: "Selected" });
return "ok";
