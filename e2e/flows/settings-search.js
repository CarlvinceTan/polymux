// Settings search lists matching tabs and sections, opens the picked one, and clearing brings the tabs back.
const app = await cua.getApp(APP);
const field = { role: "textfield", name: "Search settings" };
const clear = { role: "button", name: "Clear settings search" };
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ ...field, timeout_ms: 5000 });
await app.typeText("voice", field);
await app.waitFor({ role: "button", name: "Voice, Models", timeout_ms: 5000 });
for (const name of ["Network", "Vault", "Updates"]) {
    if ((await app.query({ role: "button", name })).length) throw new Error("Search kept the " + name + " tab");
}
await app.click({ role: "button", name: "Voice, Models" });
await app.waitFor({ role: "button", name: "Voice provider", timeout_ms: 5000 });
// A row label finds its section.
await app.click(clear);
await app.typeText("theme", field);
await app.waitFor({ role: "button", name: "Appearance, General", timeout_ms: 5000 });
// A tab's own section is listed apart from the tab.
await app.click(clear);
await app.typeText("updates", field);
await app.waitFor({ role: "button", name: "Updates, Updates", timeout_ms: 5000 });
await app.click(clear);
await app.typeText("zzq", field);
await app.waitFor({ text: "No results.", timeout_ms: 5000 });
await app.click(clear);
await app.waitFor({ role: "button", name: "Vault", timeout_ms: 5000 });
const [models] = await app.query({ role: "button", name: "Models" });
if (!models?.selected) throw new Error("Clearing search left Models unselected");
await app.click({ role: "button", name: "Close Settings" });
return "ok";
