// @fixture --verify-fixture --verify-tools --verify-profile --verify-dark
// Mesh avatars must stay responsive while the editor, profile and sidebar stacks draw together.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief" });
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Edit avatar" });
await app.click({ role: "button", name: "Food" });
for (const name of ["Cupcake", "Hamburger", "Cupcake"]) {
  await app.click({ role: "button", name });
  await app.waitFor({ role: "button", name, value: "Selected" });
  await cua.sleep(1500);
  await app.waitFor({ role: "button", name, value: "Selected" });
}
await app.click({ role: "button", name: "Close Chief’s avatar" });
await app.click({ role: "button", name: "Edit avatar" });
await app.waitFor({ role: "button", name: "Cupcake", value: "Selected" });
await app.click({ role: "button", name: "Close Chief’s avatar" });
return "ok";
