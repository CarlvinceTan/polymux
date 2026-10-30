// @fixture --verify-fixture --verify-busy --verify-thinking --verify-pins
// Group activity must not replace an individual agent's thinking state.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief, pinned chat" });
await app.click({ role: "button", name: "Chief, pinned chat" });
await app.waitFor({ text: "Mulling it over" });
await app.click({ role: "button", name: "grp-trip, Chief, Aria, Milo" });
await app.waitFor({ text: "Chief, Aria and Milo are working…" });
// Batched avatar surfaces must not intercept scrolling or row input after their anchors move.
await app.scroll({ role: "outline", name: "Sidebar" }, "down", 5);
await app.scroll({ role: "outline", name: "Sidebar" }, "up", 5);
await app.click({ role: "button", name: "Chief, pinned chat" });
await app.waitFor({ text: "Mulling it over" });
return "ok";
