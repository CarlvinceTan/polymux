// @fixture --verify-fixture --verify-cards --verify-card-cases
// Exercise varied card content, inspect actual WebKit layout for unexpected overflow, and check advanced card capabilities.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
const chief = (await app.query({ role: "button", name: "Chief" })).find((row) => row.label.startsWith("Chief, "));
if (!chief) throw new Error("Missing Chief chat");
await app.click(chief.element_token);
await app.typeText("Card", { role: "textfield", name: "Message" });
await app.pressKey("return", { role: "textfield", name: "Message" });
await app.waitFor({ role: "heading", name: "Reading", timeout_ms: 10000 });

const cases = ["text", "forms", "choices", "data", "code", "charts", "diagram", "activity", "feedback", "empty", "long", "metrics", "advanced"];
for (const [index, name] of cases.entries()) {
  if (name === "forms") {
    await app.click({ role: "button", name: "Save", exact: true });
    await app.waitFor({ text: "Saved Research workspace", timeout_ms: 10000 });
  } else if (name === "choices") {
    const option = (await app.query({ name: "Full report Findings, evidence, and recommendations", exact: true }))
      .find((row) => row.role === "AXRadioButton");
    if (!option) throw new Error("Missing delivery radio");
    await app.click(option.element_token);
    await app.waitFor({ text: "Delivery: full", timeout_ms: 10000 });
  } else if (name === "data") {
    await app.click({ role: "button", name: "Sort", exact: true });
    await app.waitFor({ text: "Sorted by workspace", timeout_ms: 10000 });
  } else if (name === "charts") {
    await app.click({ role: "button", name: "Update", exact: true });
    await app.waitFor({ text: "Updated locally", timeout_ms: 10000 });
  } else if (name === "feedback") {
    await app.click({ role: "button", name: "Archive", exact: true });
    await app.waitFor({ text: "Archived locally", timeout_ms: 10000 });
  } else if (name === "empty") {
    await app.click({ role: "button", name: "Create", exact: true });
    await app.waitFor({ text: "Example created", timeout_ms: 10000 });
  } else if (name === "advanced") {
    for (const text of ["Rendering", "Running", "9,592 primes", "2 + 3 = 5", "6 × 7 = 42"]) {
      await app.waitFor({ text, timeout_ms: 10000 });
    }
    await app.click({ role: "button", name: "Audio", exact: true });
    await app.waitFor({ text: "Audio running", timeout_ms: 10000 });
  }

  await app.click({ role: "button", name: "Inspect", exact: true });
  const state = await app.getState({ query: '{"case":', tree_format: "elements", include_screenshot: false });
  const row = state.elements?.find((row) => String(row.value || row.label || "").startsWith('{"case":'));
  if (!row) throw new Error("Missing layout report for " + name);
  const report = JSON.parse(row.value || row.label);
  if (report.case !== name || report.overflow !== 0 || report.frames !== 0) {
    throw new Error("Unexpected card layout: " + JSON.stringify(report));
  }
  if (name === "code" && !report.scroll.includes("pre")) throw new Error("Code line cannot scroll");
  await app.click({ role: "button", name: "Inspect", exact: true });
  if (index < cases.length - 1) await app.click({ role: "button", name: "Next", exact: true });
}
if ((await app.query({ text: "Part of this card didn’t work." })).length) throw new Error("Scenario script failed");
return "ok";
