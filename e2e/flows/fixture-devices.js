// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1
// The server stays online without a local app; only another paired device has Remove.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "General" });
await app.waitFor({ text: "Devices", timeout_ms: 10000 });
await app.waitFor({ role: "button", name: "Home server", timeout_ms: 10000 });
const [server] = await app.query({ role: "button", name: "Home server" });
if (!(server?.value || "").includes("Server, Online")) throw new Error(`server device not online: ${server?.value}`);
const [phone] = await app.query({ role: "button", name: "Carlvince’s iPhone" });
if (!(phone?.value || "").includes("This device") || (phone?.value || "").includes("Server")) throw new Error("current device is not identified correctly");
for (const name of ["Remove Home server", "Remove Carlvince’s iPhone"]) {
    if ((await app.query({ role: "button", name })).length) throw new Error("Offered removal of the server or this device");
}
await app.click({ role: "button", name: "Remove Spare device" });
await app.waitFor({ role: "button", name: "Spare device", gone: true, timeout_ms: 3000 });
await app.waitFor({ role: "button", name: "Home server" });
await app.click({ role: "button", name: "Add device" });
await app.typeText("invalid-link", { role: "textfield", name: "Pairing link" });
await app.click({ role: "button", name: "Join server" });
await app.waitFor({ text: "Paste a valid Polymux pairing link." });
await app.click({ role: "button", name: "Close Add device" });
return "ok";
