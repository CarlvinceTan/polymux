// @fixture --verify-fixture --verify-settings -serverAddress http://127.0.0.1:1 --verify-network-late-fail
// A device list from the cache keeps each device's last known presence and says it is cached.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Settings", timeout_ms: 10000 });
await app.click({ role: "button", name: "Settings" });
await app.click({ role: "button", name: "Network" });
await app.waitFor({ role: "button", name: "Home server", timeout_ms: 10000 });
await app.click({ role: "button", name: "Home server" });
await app.waitFor({ text: "Showing the last known devices.", timeout_ms: 10000 });
for (const [name, seen] of [["Home server", "Last seen online"], ["Carlvince’s iPhone", "Last seen online"], ["Spare device", "Last seen offline"]]) {
    const found = await app.query({ role: "button", name });
    if (!(found[0]?.value || "").includes(seen)) throw new Error(`${name} is not ${seen}: ${found[0]?.value}`);
}
return "ok";
