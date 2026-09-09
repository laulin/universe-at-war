// Decorative countdowns, progress bars and stock counters only: the server
// renders authoritative absolute times, a correct bar value and the stock it
// vouched for, and every page stays correct without JavaScript.
(() => {
  "use strict";

  const pad = (value) => String(value).padStart(2, "0");

  // The twin of figure() on the server: a player reads 1.659.181, and a counter
  // that groups its thousands differently from the page it sits in would look
  // like a second opinion.
  const figure = (value) => {
    const sign = value < 0 ? "-" : "";
    const digits = String(Math.abs(value));
    let grouped = "";
    for (let index = 0; index < digits.length; index += 1) {
      if (index > 0 && (digits.length - index) % 3 === 0) {
        grouped += ".";
      }
      grouped += digits[index];
    }
    return sign + grouped;
  };

  const format = (milliseconds) => {
    const total = Math.max(0, Math.floor(milliseconds / 1000));
    const hours = Math.floor(total / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    return `${pad(hours)}:${pad(minutes)}:${pad(total % 60)}`;
  };

  const serverTime = document.querySelector("[data-server-time]");
  const parsed = serverTime ? Date.parse(serverTime.getAttribute("datetime")) : NaN;
  const skew = Number.isNaN(parsed) ? 0 : parsed - Date.now();

  // The instant the server rendered the page, which is what every stock on it
  // was true at. Without a server time the page still ticks, from its own load.
  const origin = Number.isNaN(parsed) ? Date.now() : parsed;

  const countdowns = Array.from(document.querySelectorAll("time[data-countdown]"));
  const bars = Array.from(document.querySelectorAll("progress[data-progress]"));
  const counters = Array.from(document.querySelectorAll("[data-stock]"));
  // A form the server disabled for want of resources, and the price it waits for.
  const awaiting = Array.from(document.querySelectorAll("form[data-cost-metal]"));
  if (countdowns.length === 0 && bars.length === 0 && counters.length === 0 && awaiting.length === 0) {
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
    // A store earns at a known rate, so between two loads its figure is worth
    // extrapolating rather than leaving still. It is an estimate and stays one:
    // a debit the page never heard of is only settled by the next load.
    const elapsed = (now - origin) / 1000;
    const onHand = {};
    for (const counter of counters) {
      const stock = Number(counter.dataset.stock);
      const rate = Number(counter.dataset.rate);
      if (!Number.isFinite(stock) || !Number.isFinite(rate)) {
        continue;
      }
      const ceiling = counter.dataset.cap === undefined ? null : Number(counter.dataset.cap);
      let value = Math.floor(stock + (rate * elapsed) / 3600);
      value = Math.max(value, 0);
      if (ceiling !== null && Number.isFinite(ceiling)) {
        value = Math.min(value, ceiling);
        // A store that reached its ceiling stopped earning, which the interface
        // has to shout about the moment it happens rather than at the next load.
        counter.classList.toggle("is-full", value >= ceiling);
      }
      counter.textContent = figure(value);
      // The gauge is filled by its value, never by a style, like the bars above.
      const gauge = counter.parentElement && counter.parentElement.querySelector("meter");
      if (gauge) {
        gauge.value = value;
      }
      // Only the bar of the body being looked at is what an order spends.
      if (counter.dataset.resource) {
        onHand[counter.dataset.resource] = value;
      }
    }
    // A card the server disabled for want of resources is lifted the second the
    // stock is there, or the bar would say the price is met while the card kept
    // refusing. The page decides nothing the server will not check again when
    // the order arrives; it only stops standing in the way.
    for (const form of awaiting) {
      let batch = Number(form.dataset.ceiling) || 1;
      let covered = true;
      for (const [slug, price] of [
        ["metal", form.dataset.costMetal],
        ["crystal", form.dataset.costCrystal],
        ["deuterium", form.dataset.costDeuterium],
      ]) {
        const cost = Number(price);
        if (!Number.isFinite(cost) || cost <= 0) {
          continue;
        }
        const held = onHand[slug];
        if (!Number.isFinite(held) || held < cost) {
          covered = false;
          break;
        }
        batch = Math.min(batch, Math.floor(held / cost));
      }
      if (!covered) {
        continue;
      }
      for (const field of form.querySelectorAll("[disabled]")) {
        field.disabled = false;
      }
      // The quantity a batch may reach grows with the stock, so it is kept in
      // step rather than frozen at whatever it was when the button was lifted.
      const quantity = form.querySelector('input[type="number"]');
      if (quantity && form.dataset.ceiling) {
        quantity.max = String(batch);
      }
    }
    if (passed) {
      askForRefresh();
    }
  };

  // A holding time belongs to one mission out of eight, and shown beside the
  // seven it means nothing to it reads like a field the player forgot. The
  // server is unmoved either way: it asks for the time when the mission needs
  // one and ignores it otherwise, so a page without JavaScript merely shows a
  // field too many.
  const missionField = document.getElementById("mission");
  const holdField = document.getElementById("hold-field");
  if (missionField && holdField) {
    const showHold = () => {
      holdField.hidden = missionField.value !== "hold";
    };
    missionField.addEventListener("change", showHold);
    showHold();
  }

  tick();
  window.setInterval(tick, 1000);
})();
