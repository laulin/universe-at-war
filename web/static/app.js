// Decorative countdowns only: the server renders authoritative absolute times
// and every page stays correct without JavaScript.
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
  if (countdowns.length === 0) {
    return;
  }

  const tick = () => {
    const now = Date.now() + skew;
    for (const node of countdowns) {
      const deadline = Date.parse(node.getAttribute("datetime"));
      if (Number.isNaN(deadline)) {
        continue;
      }
      const remaining = deadline - now;
      node.textContent = remaining <= 0 ? node.dataset.done || "terminé" : format(remaining);
    }
  };

  tick();
  window.setInterval(tick, 1000);
})();
