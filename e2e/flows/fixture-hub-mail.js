// @fixture --verify-fixture --verify-plugins --verify-hub-mail
// Mail is one source with a mailbox dropdown. Reading, filters, search, moving and batches all reach the mailbox, and mail joins the combined list.
let app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Hub", timeout_ms: 10000 });
await app.click({ role: "button", name: "Hub" });
await app.waitFor({ role: "button", name: "Mail" });
await app.waitFor({ role: "button", name: "Q3 numbers", timeout_ms: 8000 });
await app.click({ role: "button", name: "Mail" });
await app.waitFor({ role: "button", name: "Invoice 42" });
if ((await app.query({ role: "button", name: "me@example.test" })).length) throw new Error("Each mailbox has its own source");

const mailbox = { role: "popupbutton", name: "Mailbox" };
await app.click(mailbox);
app = await pick(app, "work@example.test", mailbox);
await app.waitFor({ role: "button", name: "Deploy window" });
await app.click(mailbox);
app = await pick(app, "me@example.test", mailbox);
await app.waitFor({ role: "button", name: "Invoice 42" });

await app.click({ role: "button", name: "Q3 numbers" });
await app.waitFor({ text: "To: Me, team@example.test" });
await app.waitFor({ text: "Cc: Sam Lee" });
await app.waitFor({ text: "Inbox – Me" });
await app.waitFor({ text: "Conversation · 2 messages" });
for (const name of ["Reply", "Reply all", "Forward", "Archive", "Junk", "Trash", "Move to folder", "Flag", "Mark unread"]) {
  if (!(await app.query({ role: "button", name })).length && !(await app.query({ role: "menubutton", name })).length) throw new Error(`${name} is missing`);
}
if ((await app.query({ role: "button", name: "Delete forever" })).length) throw new Error("Delete forever shows outside Trash and Junk");

const filter = { role: "menubutton", name: "Filter messages" };
await app.click(filter);
app = await pick(app, "Flagged", filter);
await app.waitFor({ role: "button", name: "Newsletter", gone: true });
await app.waitFor({ role: "button", name: "This week", gone: true });
await app.waitFor({ role: "button", name: "Invoice 42" });
await app.click(filter);
app = await pick(app, "All", filter);
await app.waitFor({ role: "button", name: "This week" });

await app.typeText("invoice", { role: "textfield", name: "Search mail" });
await app.waitFor({ role: "button", name: "This week", gone: true });
await app.waitFor({ role: "button", name: "Invoice 42" });
await app.setValue({ role: "textfield", name: "Search mail" }, "");
await app.waitFor({ role: "button", name: "This week" });

await app.click({ role: "button", name: "This week" });
await app.waitFor({ role: "button", name: "Archive" });
await app.click({ role: "button", name: "Trash" });
await app.waitFor({ role: "button", name: "This week", gone: true });

const folder = { role: "popupbutton", name: "Folder" };
await app.click(folder);
app = await pick(app, "Trash", folder);
await app.waitFor({ role: "button", name: "This week" });
await app.click({ role: "button", name: "This week" });
await app.waitFor({ role: "button", name: "Delete forever" });
await app.click({ role: "button", name: "Delete forever" });
await app.waitFor({ text: "No messages." });

await app.click(folder);
app = await pick(app, "Junk", folder);
await app.click({ role: "button", name: "Broken message" });
await app.waitFor({ text: "Couldn’t open this message." });

await app.click(folder);
app = await pick(app, "Archive", folder);
await app.waitFor({ role: "button", name: "Old letter 0" });
for (let i = 0; ; i++) {
  const rows = (await app.query({ role: "button" })).filter(item => item.label?.startsWith("Old letter"));
  if (rows.some(item => item.label === "Old letter 59")) break;
  if (i > 60) throw new Error("The list stopped loading");
  await app.scroll(rows.at(-1).element_token, "down", 30);
  await cua.sleep(150);
}
await app.waitFor({ role: "button", name: "Back to top" });
await app.click({ role: "button", name: "Back to top" });
await app.waitFor({ role: "button", name: "Old letter 0" });

await app.click({ role: "button", name: "All" });
await app.waitFor({ role: "button", name: "Deploy window", timeout_ms: 8000 });
await app.click({ role: "button", name: "Deploy window" });
await app.waitFor({ text: "Inbox – Work" });
return "ok";
