---
name: context
description: Read what's open on this server's computer before you act on it or guess about it.
---

# Context

The Context tools answer "what does the user have open right now?" on the computer this Polymux server runs on, so you don't guess which browser, whether a page or app is already open, or whether someone is at the computer. They only read: nothing is launched, activated or focused. Call them through codemode, such as `text(await tools.context__summary({}))`.

- **Read once at the start** of any task that touches an app, a window, a web page or a nearby device: `context__summary` gives the front app and window, input idle time, prompts on screen, every app, window and tab, devices and the network.
- **Find before you open.** If the page or app is already open (`context__tabs` with a query, `context__windows` with an app), work there instead of opening another. Pass the pid and window id to Computer Use.
- **The front app is where the user is working.** With recent input, don't act in that window without reason, and never bring something else to the front.
- **Prompts first.** If a dialog or system prompt shows, deal with it, or tell the user, before acting in that app.
- **Private by default.** Titles and URLs can be personal: use what the task needs and don't repeat the rest.

If tabs show as unavailable, the user has to allow the Polymux app to control that browser in System Settings → Privacy & Security → Automation.
