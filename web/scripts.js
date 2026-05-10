import "./node_modules/datastar/bundles/datastar.js";

const healthOrder = { healthy: 0, unknown: 1, down: 2 };

let focusedCardId = "";
let currentGrouped = false;
const collapsedGroups = new Set();

window.sortList = function(sortKey) {
  const list = document.getElementById("proxy-list");
  if (!list) return;

  const key = sortKey || "name";
  const items = [...list.querySelectorAll(".proxy")].sort((a, b) => {
    switch (key) {
      case "status":
        return (a.dataset.status || "").localeCompare(b.dataset.status || "");
      case "provider":
        return (a.dataset.provider || "").localeCompare(b.dataset.provider || "");
      case "health": {
        const ah = healthOrder[a.dataset.health] ?? 3;
        const bh = healthOrder[b.dataset.health] ?? 3;
        if (ah !== bh) return ah - bh;
        return a.id.localeCompare(b.id);
      }
      default:
        return a.id.localeCompare(b.id);
    }
  });

  items.forEach(item => list.appendChild(item));

  if (currentGrouped) {
    applyGrouping();
  }
};

function capitalize(s) {
  if (!s) return s;
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function applyGrouping() {
  const list = document.getElementById("proxy-list");
  if (!list) return;

  list.querySelectorAll(".proxy-group").forEach(g => {
    g.querySelectorAll(".proxy").forEach(c => list.appendChild(c));
    g.remove();
  });

  const cards = [...list.querySelectorAll(".proxy")];
  const groups = new Map();

  for (const card of cards) {
    const raw = (card.dataset.category || "").trim();
    const cat = raw || "Ungrouped";
    if (!groups.has(cat)) groups.set(cat, []);
    groups.get(cat).push(card);
  }

  for (const [cat, catCards] of groups) {
    const group = document.createElement("div");
    group.className = "proxy-group";
    group.dataset.category = cat;

    const header = document.createElement("div");
    header.className = "proxy-group-header";
    header.innerHTML = `<span class="proxy-group-name">${capitalize(cat)}</span><span class="proxy-group-count badge badge-xs badge-ghost">${catCards.length}</span><svg class="proxy-group-chevron" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>`;

    header.addEventListener("click", () => {
      if (collapsedGroups.has(cat)) {
        collapsedGroups.delete(cat);
      } else {
        collapsedGroups.add(cat);
      }
      const items = group.querySelector(".proxy-group-items");
      items.classList.toggle("collapsed", collapsedGroups.has(cat));
      group.classList.toggle("is-collapsed", collapsedGroups.has(cat));
    });

    const items = document.createElement("div");
    items.className = "proxy-group-items";
    if (collapsedGroups.has(cat)) {
      items.classList.add("collapsed");
      group.classList.add("is-collapsed");
    }

    catCards.forEach(c => items.appendChild(c));

    group.appendChild(header);
    group.appendChild(items);
    list.appendChild(group);
  }
}

function removeGrouping() {
  const list = document.getElementById("proxy-list");
  if (!list) return;

  list.querySelectorAll(".proxy-group").forEach(g => {
    g.querySelectorAll(".proxy").forEach(c => list.appendChild(c));
    g.remove();
  });
}

window.groupList = function(grouped, sortKey) {
  currentGrouped = !!grouped;

  if (currentGrouped) {
    applyGrouping();
  } else {
    removeGrouping();
  }
};

window.handleKeyboard = function(evt) {
  const tag = evt.target.tagName;
  const isInput = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";

  if (evt.ctrlKey && evt.key === "f") {
    evt.preventDefault();
    document.getElementById("searchInput").focus();
    return;
  }

  if (isInput) return;

  if (evt.key === "/") {
    evt.preventDefault();
    document.getElementById("searchInput").focus();
    return;
  }

  if (evt.key === "Escape") {
    const openModal = document.querySelector("dialog[open]");
    if (openModal) {
      openModal.close();
      return;
    }
    const searchInput = document.getElementById("searchInput");
    if (searchInput && searchInput.value) {
      searchInput.value = "";
      searchInput.dispatchEvent(new Event("input", { bubbles: true }));
      return;
    }
    clearFocused();
    return;
  }

  const visibleCards = getVisibleCards();

  if (evt.key === "j") {
    evt.preventDefault();
    moveFocus(visibleCards, 1);
    return;
  }

  if (evt.key === "k") {
    evt.preventDefault();
    moveFocus(visibleCards, -1);
    return;
  }

  if (evt.key === "Enter") {
    const focused = document.querySelector(".proxy.focused");
    if (focused) {
      const openLink = focused.querySelector(".openbtn a");
      if (openLink) openLink.click();
    }
    return;
  }

  if (evt.key === "i") {
    const focused = document.querySelector(".proxy.focused");
    if (focused) {
      const modalBtn = focused.querySelector(".card-title button");
      if (modalBtn) modalBtn.click();
    }
    return;
  }
};

function getVisibleCards() {
  const list = document.getElementById("proxy-list");
  if (!list) return [];
  return [...list.querySelectorAll(".proxy")].filter(c => {
    if (c.style.display === "none") return false;
    const parent = c.closest(".proxy-group-items");
    if (parent && parent.classList.contains("collapsed")) return false;
    return true;
  });
}

function clearFocused() {
  const prev = document.querySelector(".proxy.focused");
  if (prev) prev.classList.remove("focused");
  focusedCardId = "";
}

function moveFocus(cards, direction) {
  let idx = cards.findIndex(c => c.id === focusedCardId);

  clearFocused();

  if (idx === -1) {
    idx = direction > 0 ? 0 : cards.length - 1;
  } else {
    idx += direction;
  }

  if (idx < 0 || idx >= cards.length) return;

  const card = cards[idx];
  card.classList.add("focused");
  focusedCardId = card.id;
  card.scrollIntoView({ block: "nearest", behavior: "smooth" });
}

window.showProxyNotification = function(name, status) {
  if (Notification.permission !== 'granted') return;
  const el = document.getElementById(name);
  const label = el ? el.querySelector('.card-title span')?.textContent : name;
  new Notification(`TSDProxy: ${label}`, {
    body: `Status changed to ${status}`,
    icon: '/icons/tsdproxy.svg',
  });
}

window.requestNotifications = function() {
  if ('Notification' in window && Notification.permission === 'default') {
    Notification.requestPermission();
  }
}
