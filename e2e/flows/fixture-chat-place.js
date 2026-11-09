// @fixture --verify-fixture --verify-history
// A chat left partway up reopens where it was; scrolled back to the end, it reopens there.
const app = await cua.getApp(APP);
const row = async (name) => {
  for (let i = 0; i < 60; i++) {
    const match = (await app.query({ role: "button", name })).find(item => item.label?.startsWith(name + ", "));
    if (match) return match;
    await cua.sleep(250);
  }
  throw new Error(name + " row is missing");
};
const seen = async () => (await app.query({ role: "button", name: "History message" }))
  .filter(item => item.label?.startsWith("History") && item.frame && item.frame.y + item.frame.h > 100 && item.frame.y < 900)
  .map(item => Number(item.label.match(/(\d+)/)[1]));
const open = async (name) => { await app.click((await row(name)).element_token); await cua.sleep(800); };
await open("t-long");
await app.waitFor({ role: "button", name: "History message 150", timeout_ms: 8000 });
await app.getState({});
for (let i = 0; i < 4; i++) await app.scroll([760, 284], "up", 5);
await cua.sleep(600);
const left = await seen();
if (!left.length || left.includes(150)) throw new Error("The chat did not scroll up: " + left);
await open("Chief");
await open("t-long");
const back = await seen();
if (back.includes(150) || !back.some(value => left.includes(value))) throw new Error("Lost the chat's place: " + left + " then " + back);
await app.click({ role: "button", name: "Scroll to latest" });
await cua.sleep(900);
await open("Chief");
await open("t-long");
if (!(await seen()).includes(150)) throw new Error("A chat left at its end didn't reopen there");
return "ok";
