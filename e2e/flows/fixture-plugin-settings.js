// @fixture --verify-fixture --verify-plugins --verify-plugins-empty
// Plugins sits below Vault; a marketplace install asks first, then joins the switcher.
const app = await cua.getApp(APP);
await app.click({ role: "button", name: "Settings" });
const [vault] = await app.query({ role: "button", name: "Vault" });
const [plugins] = await app.query({ role: "button", name: "Plugins" });
if (!vault || !plugins || plugins.frame.y <= vault.frame.y) throw new Error("Plugins isn't below Vault");
await app.click(plugins.element_token);
await app.waitFor({ text: "No plugins yet." });
if ((await app.query({ role: "button", name: "Hub" })).length) throw new Error("A switcher appeared with no plugins");
await app.click({ role: "button", name: "Marketplace" });
await app.click({ role: "button", name: "Install Hub" });
await app.waitFor({ text: "Version 1.0.0 from CarlvinceTan/polymux@1234567. It runs on your server." });
await app.click({ role: "button", name: "Install" });
await app.waitFor({ text: "Installed" });
await app.click({ role: "button", name: "Installed" });
await app.waitFor({ role: "button", name: "Hub" });
await app.click({ role: "button", name: "Close Settings" });
await app.waitFor({ role: "button", name: "Chats" });
return "ok";
