// @fixture --verify-fixture --verify-permissions --verify-onboarding
// The first-run walkthrough goes from the welcome to the permissions bots need, counted from the server's answer.
const app = await cua.getApp(APP);
await app.waitFor({ role: "statictext", name: "Get Started with Polymux", timeout_ms: 10000 });
await app.click({ role: "button", name: "Continue" });
await app.waitFor({ role: "statictext", name: "System Permissions", timeout_ms: 5000 });

// The fixture server reports Screen Recording granted, so the count covers its two permissions too.
const counted = async () => (await app.query({ role: "statictext" }))
  .map((item) => item.value || item.label || "")
  .find((text) => /^[12] of 3 granted$/.test(text));
let summary;
for (let i = 0; i < 20 && !summary; i++) {
  summary = await counted();
  if (!summary) await cua.sleep(250);
}
if (!summary) throw new Error("The permissions count doesn't include the server's answer");

await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "statictext", name: "Get Started with Polymux", timeout_ms: 5000 });
await app.click({ role: "button", name: "Continue" });
await app.click({ role: "button", name: "Done" });
for (let i = 0; i < 20 && (await app.query({ role: "statictext", name: "System Permissions" })).length; i++) await cua.sleep(250);
if ((await app.query({ role: "statictext", name: "System Permissions" })).length) throw new Error("The walkthrough stayed open");

// Settings keeps a way back to the same list.
await app.click({ role: "button", name: "Settings" });
await app.waitFor({ role: "button", name: "Grant permissions", timeout_ms: 10000 });
await app.waitFor({ role: "statictext", name: summary, timeout_ms: 5000 });
