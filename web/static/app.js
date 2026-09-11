// Decorative countdowns, bars, counters and conveniences only: the server
// renders authoritative absolute times, a correct bar value, the stock it
// vouched for and every figure a form is bounded by, and every page stays
// correct without JavaScript. A control that only works with a script is
// rendered away and revealed here, so a page without one is never offered a
// button that does nothing.
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

  // Everything below this point is wired before the guard that follows it: what
  // comes after that guard only runs on a page that ticks, and a form is not
  // that kind of page.

  // The resources-page title is the rename control. The real form is rendered
  // open as a no-script fallback; once this convenience is available, the title
  // replaces it until clicked. Enter submits naturally, while Escape or leaving
  // the editor restores the last server-confirmed value.
  const rename = document.querySelector("[data-body-rename]");
  if (rename) {
    const trigger = rename.querySelector("[data-body-rename-trigger]");
    const form = rename.querySelector("[data-body-rename-form]");
    const input = rename.querySelector("[data-body-rename-input]");
    const cancel = rename.querySelector("[data-body-rename-cancel]");
    const close = () => {
      input.value = input.defaultValue;
      form.hidden = true;
      trigger.hidden = false;
      cancel.hidden = true;
    };
    const open = () => {
      trigger.hidden = true;
      form.hidden = false;
      cancel.hidden = false;
      input.focus();
      input.select();
    };
    trigger.addEventListener("click", open);
    cancel.addEventListener("click", close);
    form.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        trigger.focus();
      }
    });
    form.addEventListener("focusout", () => {
      window.setTimeout(() => {
        if (!form.contains(document.activeElement)) {
          close();
        }
      }, 0);
    });
    close();
  }

  // The server writes the ceiling of a field on the field itself, so a button
  // that fills it to the brim needs no figure of its own. It is rendered away
  // and revealed here: without a script there is no button rather than a dead
  // one. A field the server disabled arrives disabled too, and is freed with
  // its field once the stock is there — never before, or it would fill in the
  // ceiling of a batch nobody can pay for yet.
  for (const button of document.querySelectorAll("button[data-max-for]")) {
    const field = document.getElementById(button.dataset.maxFor);
    if (!field) {
      continue;
    }
    button.hidden = false;
    button.addEventListener("click", () => {
      field.value = field.max;
      field.dispatchEvent(new Event("input", { bubbles: true }));
    });
  }

  // How much a fleet can still carry. On the send form the hold is the sum of
  // what the chosen ships hold, which is the very arithmetic the domain uses;
  // on the confirmation the server has already taken the fuel out of it and
  // says so in one figure. Either way the loading is the player's own three
  // numbers, so the remainder is arithmetic and never a rule.
  const hold = document.querySelector("[data-hold]");
  if (hold) {
    const holds = Array.from(document.querySelectorAll("[data-cargo]"));
    const loads = Array.from(document.querySelectorAll("[data-load]"));
    const shown = (name) => hold.querySelector(`[data-hold-${name}]`);
    const measure = () => {
      const fixed = Number(hold.dataset.hold);
      const capacity = Number.isFinite(fixed) && hold.dataset.hold !== ""
        ? fixed
        : holds.reduce((total, ship) => total + Number(ship.dataset.cargo || 0) * Number(ship.value || 0), 0);
      const loaded = loads.reduce((total, field) => total + Number(field.value || 0), 0);
      const free = capacity - loaded;
      for (const [name, value] of [["capacity", capacity], ["loaded", loaded], ["free", free]]) {
        const node = shown(name);
        if (node) {
          node.textContent = figure(value);
        }
      }
      hold.classList.toggle("is-over", free < 0);
      // The ceiling of one field is what the stores hold, lowered by the room
      // the other two have already taken. The server's figure is only ever
      // lowered here: it is a floor the page has no business raising.
      for (const field of loads) {
        const stock = Number(field.dataset.stockMax);
        if (!Number.isFinite(stock)) {
          continue;
        }
        const room = capacity - (loaded - Number(field.value || 0));
        field.max = String(Math.max(0, Math.min(stock, room)));
      }
    };
    for (const field of holds.concat(loads)) {
      field.addEventListener("input", measure);
    }
    measure();
  }

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

  tick();
  window.setInterval(tick, 1000);
})();
