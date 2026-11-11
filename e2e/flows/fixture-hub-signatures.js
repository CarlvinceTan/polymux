// @fixture --verify-fixture --verify-plugins --verify-hub-mail
// Mailboxes show their state and servers, signatures are managed per mailbox with a default, and a new one reaches the composer.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Mail" });
app = await pick(app, "Add account");
await app.waitFor({ role: "button", name: "Edit me@example.test" });
await app.waitFor({ text: "imap.example.test:993 · smtp.example.test:587 · Password" });
await app.click({ role: "button", name: "Edit me@example.test" });
await app.waitFor({ role: "textfield", name: "IMAP server" });
if ((await app.query({ role: "textfield", name: "IMAP server" }))[0]?.value !== "imap.example.test") throw new Error("Editing doesn't show the servers");
await app.typeText("Carl Tan", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Signatures for me@example.test" });
await app.click({ role: "button", name: "Signatures for me@example.test" });
await app.waitFor({ role: "textarea", name: "Signature" });
await app.click({ role: "button", name: "Italic" });
for (let i = 0; !(await app.query({ role: "button", name: "Italic" }))[0]?.selected; i++) {
  if (i > 30) throw new Error("The toolbar doesn't reach the editor");
  await cua.sleep(100);
}
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "button", name: "Signatures for work@example.test" });
await app.click({ role: "button", name: "Signatures for work@example.test" });
await app.waitFor({ text: "No signatures yet." });
await app.click({ role: "button", name: "Add signature" });
await app.waitFor({ role: "textfield", name: "Name" });
await app.typeText("Sales", { role: "textfield", name: "Name" });
await app.click({ role: "button", name: "Save" });
await app.waitFor({ role: "button", name: "Signatures for work@example.test" });
await app.click({ role: "button", name: "Close Accounts" });
await app.click({ role: "button", name: "Mail" });
const mailbox = { role: "popupbutton", name: "Mailbox" };
await app.waitFor(mailbox);
await app.click(mailbox);
app = await pick(app, "work@example.test", mailbox);
await app.waitFor({ role: "button", name: "Deploy window" });
await app.click({ role: "button", name: "New mail" });
await app.waitFor({ role: "popupbutton", name: "Signature" });
if ((await app.query({ role: "popupbutton", name: "Signature" }))[0]?.value !== "Sales") throw new Error("A mailbox's first signature is its default");
return "ok";
