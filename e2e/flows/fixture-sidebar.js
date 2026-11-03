// @fixture --verify-fixture --verify-pins
// The seeded agent rows load.
const app = await cua.getApp(APP);
const row = async (name) => (await app.query({ role: "button", name })).some((item) => item.label.startsWith(name + ", "));
for (let i = 0; i < 60 && !(await row("Chief")); i++) await cua.sleep(500);
for (const name of ["Chief", "Aria", "Milo", "Nova"]) {
  if (!(await row(name))) throw new Error(name + " row is missing");
}
return "ok";
