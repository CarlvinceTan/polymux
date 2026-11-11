// @fixture --verify-fixture --verify-plugins --verify-graph --verify-graph-video
// The map: whoever's working stands at their step, a step opens on what it made and each of those opens on its own,
// a district's banner lists its steps, a handoff says what it is, and nothing here starts or edits work.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Graph Visualiser" });
await app.click({ role: "button", name: "Graph Visualiser" });
await app.waitFor({ role: "button", name: "Show diagram" });
await app.waitFor({ role: "button", name: "Characters, Step" });
await app.waitFor({ role: "button", name: "Milo, Drawing Robo" });
await app.waitFor({ role: "button", name: "Storyboard, Step" });

// A step opens on what it made, and each of those opens on its own and goes back.
await app.click({ role: "button", name: "Characters, Step" });
await app.waitFor({ role: "button", name: "Close details" });
await app.waitFor({ text: "Made here" });
await app.click({ role: "button", name: "Mia" });
await app.waitFor({ role: "button", name: "Back" });
// A picture opens full size, and closes again.
await app.click({ role: "button", name: "Open Mia full size" });
await app.waitFor({ role: "button", name: "Close" });
await app.click({ role: "button", name: "Close" });
await app.waitFor({ role: "button", name: "Close", gone: true });
await app.click({ role: "button", name: "Back" });
await app.waitFor({ text: "Made here" });
await app.click({ role: "button", name: "Close details" });
await app.waitFor({ role: "button", name: "Close details", gone: true });

// A district's banner goes there and lists what's in it.
await app.click({ role: "button", name: "Launch film, district" });
await app.waitFor({ text: "Steps" });
await app.waitFor({ text: "Progress" });
await app.click({ role: "button", name: "Close details" });
await app.click({ role: "button", name: "Fit to view" });

// Someone at the top goes to them, wherever the map is looking.
await app.click({ role: "button", name: "Nova, Rendering shot 3 of 6", nth: 0 });
await app.waitFor({ text: "Now" });
await app.click({ role: "button", name: "Close details" });

// A handoff in the air says what it is.
// Whichever handoff is in the air for Milo: only two fly at once.
const tags = (await app.query({ role: "button", name: "for Milo" })).filter((item) => / for Milo$/.test(item.label || ""));
if (!tags.length) throw new Error("no handoff for Milo was in the air");
await app.click(tags[0].element_token);
await app.waitFor({ text: "On its way" });
await app.click({ role: "button", name: "Close details" });
await app.waitFor({ role: "button", name: "Close details", gone: true });

// It's a view: nothing here starts or edits work.
for (const name of ["Run", "Stop", "Add node", "Delete node", "Retry"]) {
  if ((await app.query({ role: "button", name })).length) throw new Error(`${name} shouldn't exist`);
}

// The diagram of the same work is one button away, and so is the way back.
await app.click({ role: "button", name: "Show diagram" });
await app.waitFor({ role: "button", name: "Show map" });
await app.click({ role: "button", name: "Show map" });
await app.waitFor({ role: "button", name: "Show diagram" });
return "ok";
