// The main window opens with its search field and toolbar buttons. APP is injected by scripts/run-e2e.sh.
const app = await cua.getApp(APP);
await app.waitFor({ role: "textfield", name: "Search" });
await app.waitFor({ role: "button", name: "New chat" });
await app.waitFor({ role: "button", name: "Settings" });
return "ok";
