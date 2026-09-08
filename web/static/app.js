// Decorative countdowns and progress bars only: the server renders
// authoritative absolute times and a correct bar value, and every page stays
// correct without JavaScript.
(() => {
  "use strict";

  const pad = (value) => String(value).padStart(2, "0");

  const format = (milliseconds) => {
    const total = Math.max(0, Math.floor(milliseconds / 1000));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    return `${pad(hours)}:${pad(minutes)}:${pad(total % 60)}`;
  };

  const serverTime = document.querySelector("[data-server-time]");
  const parsed = serverTime ? Date.parse(serverTime.getAttribute("datetime")) : NaN;
  const skew = Number.isNaN(parsed) ? 0 : parsed - Date.now();

  const countdowns = Array.from(document.querySelectorAll("time[data-countdown]"));
  const bars = Array.from(document.querySelectorAll("progress[data-progress]"));
  if (countdowns.length === 0 && bars.length === 0) {
    return;
  }

  // When the earliest deadline on the page passes, the server knows something
  // this page does not. One reload is asked for, once, a moment later so the
  // event has been settled. Without JavaScript the page stays correct: it shows
  // absolute server times and the visitor reloads when they choose to.
  let reloadAsked = false;
  const askForRefresh = () => {
    if (reloadAsked || document.hidden) {
      return;
    }
    reloadAsked = true;
    window.setTimeout(() => window.location.reload(), 2000);
  };

  const tick = () => {
    const now = Date.now() + skew;
    let passed = false;
    for (const node of countdowns) {
      const deadline = Date.parse(node.getAttribute("datetime"));
      if (Number.isNaN(deadline)) {
        continue;
      }
      const remaining = deadline - now;
      node.textContent = remaining <= 0 ? node.dataset.done || "terminé" : format(remaining);
      if (remaining <= 0 && node.dataset.refresh === "page") {
        passed = true;
      }
    }
    // A bar is filled by its value, never by a style: the policy refuses inline
    // styles, and the server already rendered the value this loop continues.
    for (const bar of bars) {
      const deadline = Date.parse(bar.dataset.end);
      const span = Number(bar.dataset.span);
      if (Number.isNaN(deadline) || !Number.isFinite(span) || span <= 0) {
        continue;
      }
      const done = span - (deadline - now) / 1000;
      bar.value = Math.min(Math.max(done, 0), span);
    }
    if (passed) {
      askForRefresh();
    }
  };

  tick();
  window.setInterval(tick, 1000);
})();
