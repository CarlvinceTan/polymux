// The theme lives in settings, which are only readable over IPC once the
// renderer bundle has mounted — far too late for the startup splash, which
// paints with the document. This runs in <head>, before the first paint, and
// restores the last resolved choice so the splash opens in the right theme
// instead of flashing into it. `lib/theme.ts` writes the value it reads here.
(function () {
  try {
    var stored = localStorage.getItem("polymux.theme");
    // A fresh profile has nothing stored, and 'system' is what it will be given
    // — so resolve that here rather than leaving the root bare. The stylesheet
    // can dress an unthemed root, but the attribute would still arrive later,
    // on mount, and that write is a recalc of the whole document landing in the
    // middle of the splash's slide. Setting it before first paint means the
    // renderer's own applyTheme has nothing left to change.
    var mode =
      stored === "light" || stored === "dark" || stored === "system"
        ? stored
        : "system";
    var dark =
      mode === "dark" ||
      (mode === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
  } catch (error) {
    // Storage can be denied outright (sandboxed previews). The stylesheet
    // falls back to the system preference while no theme is set, so a launch
    // without this hint still opens in a sensible colour.
  }

  // Same story for the interface language: it lives in settings, and the one
  // thing the document must know before it paints is which way round it goes.
  // Arabic arriving on mount would re-lay-out the whole page mid-splash, so the
  // last resolved choice is restored here. `lib/i18n/index.ts` writes it.
  try {
    var language = localStorage.getItem("polymux.language") || "system";
    if (language === "system") {
      var requested =
        (navigator.languages && navigator.languages.length
          ? navigator.languages
          : [navigator.language])[0] || "en";
      language = String(requested).split("-")[0];
    }
    document.documentElement.lang = language;
    document.documentElement.dir = language === "ar" ? "rtl" : "ltr";
  } catch (error) {
    // Without the hint the document stays as the markup left it, English and
    // left to right, and the renderer corrects it on mount.
  }

  // The sequence starts when the splash is actually on screen, so that all of
  // it is seen. In the app that moment belongs to the main process, which holds
  // the window back until its first frame and sets this attribute as it shows
  // it (see main.ts) — a hidden Electron window still reports its document as
  // visible, so the page cannot tell. In a browser the page is on screen as
  // soon as it paints. The timeout is a failsafe: a splash that never animates
  // would be a splash that never leaves.
  var root = document.documentElement;
  // A document loaded into the window that already played the shell's
  // animation to its settled lockup (main.ts sets `splashSettled` on that
  // navigation). This paint must open on that same settled pose — the swap is
  // invisible only if the first frame here matches the last frame there — so
  // the attribute is set before first paint and the whole play/done machinery
  // below is skipped: starting it would replay the sequence from the top, and
  // its watch flipping the value to "done" would do the same through the
  // stylesheet. App.svelte treats "settled" as the sequence already watched.
  // The dead-bundle sweep at the bottom still runs: this cover, like any
  // other, must not outlive a bundle that never mounts.
  var startupParams = new URLSearchParams(location.search);
  var settled = startupParams.has("splashSettled");
  // Warm windows must skip the cover before the renderer bundle mounts.
  // Keep this separate from data-splash, which main sets when showing a window.
  if (startupParams.get("coldStart") === "0" && !settled) {
    root.dataset.skipStartup = "true";
    return;
  }
  if (settled) root.dataset.splash = "settled";
  else startSequence();
  function startSequence() {
    function play() {
      if (root.dataset.splash) return;
      root.dataset.splash = "playing";
    }
    // Done means the cover may lift as soon as the app is ready. The app asks
    // for this state rather than timing the sequence itself, so it cannot matter
    // whether it mounts before or after the sequence finishes.
    function done() {
      if (root.dataset.splash === "done") return;
      root.dataset.splash = "done";
      document.dispatchEvent(new Event("polymux:splash-done"));
    }
    // The last beat of the sequence is the lockup settling into place and holding
    // there. Its end is what says the animation has been watched through.
    document.addEventListener("animationend", function (event) {
      if (event.animationName === "startup-brand-in") done();
    });
    // …and a ceiling on it, because that event is the only thing standing between
    // a splash and never leaving: an animation dropped for any reason — a mark
    // that failed to parse, a motion setting flipped mid-run — would take the
    // whole cover down with it. The clock starts when the sequence does, whoever
    // started it, which is why this watches the attribute rather than sitting in
    // `play` (main sets it directly, over IPC, and never calls that).
    var watch = new MutationObserver(function () {
      if (!root.dataset.splash) return;
      watch.disconnect();
      setTimeout(done, 3400);
    });
    watch.observe(root, { attributes: true, attributeFilter: ["data-splash"] });
    // Reduced motion removes the sequence altogether, so there is nothing to wait
    // for and no animationend coming.
    //
    // The app's own timeout has to sit behind main's, not in front of it: main
    // shows the window on its first frame or on a 4s deadline, whichever comes
    // first, and sets the attribute as it does. At 3s this fired first whenever
    // the renderer took a while to reach that frame — which in development it
    // always does, the bundle coming off a dev server — and the whole sequence
    // then played out behind a window that was still hidden, so the cover lifted
    // on a lockup nobody had seen move. 6s is past every path main can take, and
    // still well inside the dead-bundle sweep below.
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) done();
    else if (!window.polymux) play();
    else setTimeout(play, 6000);
  }

  // The app lifts the splash when it mounts. If the bundle never loads at all
  // nothing would, and a full-window cover would hide the very thing that says
  // why — dev's error overlay, or a blank page worth reporting. A mounted app
  // is left alone: its own slow-load ceiling is 8s, well inside this.
  setTimeout(function () {
    var mount = document.getElementById("app");
    var splash = document.getElementById("startup-splash");
    if (
      splash &&
      mount &&
      !mount.hasChildNodes() &&
      !new URLSearchParams(location.search).has("splashOnly")
    )
      splash.remove();
  }, 12000);
})();
