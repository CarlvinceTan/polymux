// @fixture --verify-fixture --verify-credentials
// Credential cards show their host and lifetime, and an SSH key is entered in a multiline field.
const app = await cua.getApp(APP);
async function open(name) {
  const rows = (await app.query({ role: "button", name })).filter((row) => row.label.startsWith(name + ", "));
  if (!rows.length) throw new Error("Missing chat " + name);
  await app.click(rows[0].element_token);
}
await app.waitFor({ role: "button", name: "Chief", timeout_ms: 10000 });
await open("Chief");
await app.waitFor({ text: "Trial key", timeout_ms: 10000 });
await app.waitFor({ text: "https://api.example.com", timeout_ms: 10000 });
await app.waitFor({ text: "Kept in memory only.", timeout_ms: 10000 });
await app.waitFor({ text: "Deploy key", timeout_ms: 10000 });
await app.waitFor({ text: "github.com", timeout_ms: 10000 });
const editor = { role: "textarea", name: "SSH private key" };
if (!(await app.query(editor)).length) throw new Error("SSH key field missing");
await app.setValue(editor, "-----BEGIN OPENSSH PRIVATE KEY-----\nfixture\n-----END OPENSSH PRIVATE KEY-----");
await app.click({ role: "button", name: "Save" });
for (let i = 0; i < 40 && (await app.query({ role: "statictext", name: "Credential saved" })).length < 2; i++) {
  await cua.sleep(250);
}
if ((await app.query({ role: "statictext", name: "Credential saved" })).length < 2) {
  throw new Error("the SSH key card did not save");
}
if ((await app.query(editor)).length) throw new Error("SSH key field stayed after saving");
return "ok";
