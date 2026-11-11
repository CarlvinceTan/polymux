// @fixture --verify-fixture --verify-plugins --verify-hub-mail
// A reply quotes and threads, the default signature comes with it, drafts save themselves and reopen, and sending clears the draft into Sent.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Mail" });
await app.click({ role: "button", name: "Mail" });
await app.waitFor({ role: "button", name: "Q3 numbers" });
await app.click({ role: "button", name: "Q3 numbers" });
await app.waitFor({ text: "Cc: Sam Lee" });
await app.click({ role: "button", name: "Reply" });
await app.waitFor({ role: "textfield", name: "Subject" });
const value = async name => (await app.query({ role: "textfield", name }))[0]?.value ?? "";
if (await value("Subject") !== "Re: Q3 numbers") throw new Error("A reply keeps the subject with one prefix");
if (await value("To") !== "priya.raman@example.test") throw new Error(`A reply goes to its sender: ${await value("To")}`);
const [editor] = await app.query({ role: "textarea", name: "Message" });
if (!editor?.value?.includes("Priya Raman wrote:") || !editor.value.includes("> The figures are attached.")) throw new Error("A reply quotes what it answers");
const signature = { role: "popupbutton", name: "Signature" };
if ((await app.query(signature))[0]?.value !== "Work") throw new Error("The default signature isn't chosen");

await app.typeText("Re: Q3 numbers updated", { role: "textfield", name: "Subject" });
await cua.sleep(1200);
await app.click({ role: "button", name: "Back" });
await app.waitFor({ role: "textfield", name: "Subject", gone: true });
const folder = { role: "popupbutton", name: "Folder" };
await app.click(folder);
app = await pick(app, "Drafts", folder);
await app.waitFor({ role: "button", name: "Re: Q3 numbers updated" });
await app.click({ role: "button", name: "Re: Q3 numbers updated" });
await app.waitFor({ role: "textfield", name: "Subject" });
await app.waitFor({ text: "Edit draft" });
await app.click({ role: "button", name: "Send" });
await app.waitFor({ role: "button", name: "Re: Q3 numbers updated", gone: true });
await app.click(folder);
app = await pick(app, "Sent", folder);
await app.waitFor({ role: "button", name: "Re: Q3 numbers updated" });
return "ok";
