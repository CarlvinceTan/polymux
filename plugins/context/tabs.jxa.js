// Context plugin: browser tab metadata: titles and URLs only, through each
// browser's own scripting interface. No page evaluation, no selection, no
// activation. Ported from cstack's context tool.
//
// Usage: osascript -l JavaScript -e "$(cat tabs.jxa.js)" <bundle-id> <chromium|safari>
// Always prints JSON, including on failure, so the caller never has to parse an
// error string.

function collectTabs(a, schema) {
  var warnings = [],
    windows = [],
    before = a.windows.id();
  var exact = schema === "chromium";
  function read(fn, fallback, message) {
    try {
      return fn();
    } catch (_) {
      warnings.push(message);
      return fallback;
    }
  }
  function vector(w, property, count) {
    var values = read(function () {
      return w.tabs[property]();
    }, null, "tab " + property + " unavailable");
    if (!Array.isArray(values) || values.length !== count) {
      warnings.push("tab " + property + " inventory changed or is incomplete");
      return [];
    }
    if (property !== "id" && values.some(function (v) { return typeof v !== "string"; }))
      warnings.push("some tab " + property + " values unavailable");
    return values;
  }
  before.forEach(function (id) {
    var w = a.windows.byId(id),
      tabs = [];
    var count = read(function () { return w.tabs().length; }, null, "window tabs unavailable");
    if (count === null) {
      windows.push({ window_id: String(id), tabs: [] });
      return;
    }
    var ids = exact ? vector(w, "id", count).map(String) : [];
    var titles = vector(w, schema === "chromium" ? "title" : "name", count);
    var urls = vector(w, "url", count);
    // Selection contributes only to admission, never to target identity.
    var selected = exact
      ? read(function () { return String(w.activeTab.id()); }, null, "selected tab unavailable")
      : null;
    if (exact && ids.indexOf(selected) < 0) selected = null;
    var after = exact ? vector(w, "id", count).map(String) : [];
    var unchanged = exact && ids.length === count && JSON.stringify(ids) === JSON.stringify(after);
    if (exact && !unchanged) warnings.push("tab topology changed during collection");
    if (!exact && read(function () { return w.tabs().length; }, -1, "tab count unavailable") !== count)
      warnings.push("tab topology changed during collection");
    for (var i = 0; i < count; i++) {
      var row = {
        title: typeof titles[i] === "string" ? titles[i] : "",
        url: typeof urls[i] === "string" ? urls[i] : "",
      };
      if (unchanged && /^[1-9][0-9]*$/.test(ids[i])) {
        row.tab_id = ids[i];
        row.selected = selected === null ? "unknown" : ids[i] === selected;
      }
      tabs.push(row);
    }
    windows.push({ window_id: String(id), tabs: tabs });
  });
  if (exact) {
    var seen = {};
    windows.forEach(function (w) {
      w.tabs.forEach(function (t) {
        if (!t.tab_id || seen[t.tab_id]) warnings.push("tab identity unavailable or duplicated");
        seen[t.tab_id] = true;
      });
    });
  }
  var end = read(function () { return a.windows.id(); }, [], "window inventory unavailable");
  if (JSON.stringify(before.slice().sort()) !== JSON.stringify(end.slice().sort()))
    warnings.push("window topology changed during collection");
  return {
    windows: windows,
    schema: schema,
    observed_at: Date.now() / 1000,
    tab_inventory: {
      status: warnings.length ? "partial" : "complete",
      scope: "scriptable_open_windows",
      count: windows.reduce(function (n, w) { return n + w.tabs.length; }, 0),
      exclusions: [
        "tabs not exposed by the browser scripting interface",
        "other browser processes",
      ],
    },
    warnings: warnings.filter(function (v, i, list) { return list.indexOf(v) === i; }),
  };
}

function run(args) {
  try {
    if (args.length !== 2 || ["chromium", "safari"].indexOf(args[1]) < 0)
      return JSON.stringify({ windows: [], warnings: ["supported browser scripting schema required"] });
    var a = Application(args[0]);
    if (!a.running()) return JSON.stringify({ windows: [], warnings: ["browser is no longer running"] });
    return JSON.stringify(collectTabs(a, args[1]));
  } catch (error) {
    return JSON.stringify({ windows: [], warnings: [String(error)] });
  }
}
