// @fixture --verify-fixture --verify-tools --verify-profile
// Shapes keeps its palette when browsing back from a character; new designs remain selectable.
const app = await cua.getApp(APP);
await app.waitFor({ role: "button", name: "Chief" });
const [chief] = (await app.query({ role: "button", name: "Chief" })).filter(item => item.label.startsWith("Chief, "));
if (!chief) throw new Error("Chief row is missing");
await app.click(chief.element_token);
await app.click({ role: "button", name: "Edit Chief’s profile" });
await app.click({ role: "button", name: "Edit avatar" });
await app.waitFor({ role: "button", name: "Shapes" });

async function snapshot() {
  const state = await app.getState({ include_screenshot: false, tree_format: "elements", max_elements: 250, timeout_ms: 5000 });
  if (state.truncated) throw new Error("Avatar picker lookup is incomplete");
  if (state.elements.some(item => ["Expression", "Preview"].includes(item.label) ||
      ["Expression", "Preview"].includes(item.value))) {
    throw new Error("Manual expression or preview controls are still visible");
  }
  return state.elements;
}
const colors = ["Black / White", "Brown", "Red", "Orange", "Amber", "Green", "Turquoise", "Blue", "Purple", "Pink", "Grey"];
for (const category of ["Food", "Nature", "Objects", "Animals"]) {
  await app.click({ role: "button", name: category });
  const elements = await snapshot();
  if (elements.some(item => item.role === "AXButton" && item.label === "Black / White")) {
    throw new Error(category + " shows shape colours");
  }
  const replacements = {
    Food: ["Cupcake", "Hamburger", "Popcorn"],
    Nature: ["Palm", "Snowman", "Lightning", "Blossom", "Leaf", "Sunflower"],
    Objects: ["Balloon", "Light bulb", "Hammer", "Watering can", "Gamepad", "Trophy", "Gem"],
    Animals: ["Panda", "Crab", "Elephant", "Octopus", "Giraffe", "Walrus", "Turtle", "Bat", "Dog", "Axolotl", "Mouse"],
  };
  if (elements.some(item => item.role === "AXButton" && ["Pinecone", "Lotus"].includes(item.label))) {
    throw new Error("The retired avatar is still in the picker");
  }
  for (const name of replacements[category] || []) {
    await app.click({ role: "button", name });
    await app.waitFor({ role: "button", name, value: "Selected" });
  }
  if (category === "Objects") {
    await app.click({ role: "button", name: "Balloon" });
    await app.click({ role: "button", name: "Close Chief’s avatar" });
    await app.click({ role: "button", name: "Edit avatar" });
    await app.waitFor({ role: "button", name: "Balloon", value: "Selected" });
  }
}
const animals = ["Cat", "Dog", "Rabbit", "Pig", "Deer", "Koala", "Turtle", "Frog", "Mouse", "Monkey", "Penguin", "Axolotl", "Whale", "Bull"];
const elements = await snapshot();
for (const animal of animals) {
  if (!elements.some(item => item.role === "AXButton" && item.label === animal)) throw new Error(animal + " is missing");
}
await app.click({ role: "button", name: "Octopus" });
await app.click({ role: "button", name: "Shapes" });
const shapes = await snapshot();
for (const color of colors) {
  if (!shapes.some(item => item.role === "AXButton" && item.label === color)) throw new Error(color + " swatch is missing");
}
await app.click({ role: "button", name: "Plus" });
await app.click({ role: "button", name: "Blue" });
const chosen = await snapshot();
for (const name of ["Plus", "Blue"]) {
  if (!chosen.some(item => item.role === "AXButton" && item.label === name && item.value === "Selected")) {
    throw new Error(name + " selection was lost");
  }
}
await app.click({ role: "button", name: "Close Chief’s avatar" });
await app.click({ role: "button", name: "Edit avatar" });
await app.waitFor({ role: "button", name: "Plus", value: "Selected" });
await app.waitFor({ role: "button", name: "Blue", value: "Selected" });
await app.click({ role: "button", name: "Close Chief’s avatar" });
return "ok";
