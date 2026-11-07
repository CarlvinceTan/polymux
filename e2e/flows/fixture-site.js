// @fixture --verify-fixture --verify-site --verify-dark
const app = await cua.getApp(APP);
for (const [name, prompt] of [
  ['Atlas,', 'Help me plan my afternoon: review the website, write release notes, and leave time for a walk.'],
  ['Nova,', 'Remember how I like release notes: short, changes first, no marketing language.'],
  ['Fern,', 'Every Friday at 4 pm, remind me to review the week. Use my local timezone.'],
]) {
  await app.click({role: 'button', name});
  await app.waitFor({role: 'button', name: prompt, timeout_ms: 10000});
  await app.waitFor({role: 'textfield', name: 'Message', timeout_ms: 10000});
}
return 'ok';
