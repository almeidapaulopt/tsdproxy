// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

// Minimal JS for the htmx dashboard: keyboard nav, notifications, toasts.
// All sort/filter/group/pin/rendering is server-side.

import htmx from 'htmx.org';

window.htmx = htmx;

document.addEventListener('htmx:after:swap', () => {
  const toasts = document.querySelectorAll('#toast-container .toast-alert');
  toasts.forEach((t) => {
    setTimeout(() => {
      t.classList.add('opacity-0', 'translate-x-full');
      t.addEventListener('transitionend', () => t.remove(), { once: true });
    }, 4200);
  });
});

document.addEventListener('setCompact', (evt) => {
  const list = document.getElementById('proxy-list');
  if (!list) return;
  const compact = evt.detail.value !== false;
  list.classList.toggle('compact', compact);

  document.querySelectorAll('.view-toggle-card').forEach((b) =>
    b.classList.toggle('btn-primary', !compact),
  );
  document.querySelectorAll('.view-toggle-compact').forEach((b) =>
    b.classList.toggle('btn-primary', compact),
  );
});

const sse = document.createElement('script');
sse.src = '/hx-sse.js';
sse.onload = () => htmx.process(document.body);
document.head.appendChild(sse);

let focusedCardId = '';

window.requestNotifications = function () {
  if ('Notification' in window && Notification.permission === 'default') {
    Notification.requestPermission().then(() => hideNotificationBtn());
  }
};

function hideNotificationBtn() {
  if ('Notification' in window && Notification.permission !== 'default') {
    document.querySelectorAll('.request-notifications-btn').forEach((b) => b.remove());
  }
}

hideNotificationBtn();

window.showProxyNotification = function (evt) {
  const msg = typeof evt.detail === 'string' ? evt.detail : evt.detail?.data || '';
  const sep = msg.indexOf('\x00');
  if (sep === -1) return;
  const name = msg.slice(0, sep);
  const status = msg.slice(sep + 1);
  if (Notification.permission !== 'granted') return;
  const el = document.getElementById(name);
  const label = el ? el.querySelector('.card-title span')?.textContent : name;
  new Notification('TSDProxy: ' + label, {
    body: 'Status changed to ' + status,
    icon: '/icons/tsdproxy.svg',
  });
};

window.addEventListener('keydown', (evt) => {
  const tag = evt.target.tagName;
  const isInput = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';

  if (evt.ctrlKey && evt.key === 'f') {
    evt.preventDefault();
    document.getElementById('searchInput')?.focus();
    return;
  }

  if (isInput) return;

  if (evt.key === '/') {
    evt.preventDefault();
    document.getElementById('searchInput')?.focus();
    return;
  }

  if (evt.key === 'Escape') {
    const root = document.getElementById('modal-root');
    if (root && root.innerHTML.trim()) {
      root.innerHTML = '';
      return;
    }
    const searchInput = document.getElementById('searchInput');
    if (searchInput && searchInput.value) {
      searchInput.value = '';
      searchInput.dispatchEvent(new Event('input', { bubbles: true }));
      return;
    }
    clearFocused();
    return;
  }

  const visibleCards = getVisibleCards();

  if (evt.key === 'j') {
    evt.preventDefault();
    moveFocus(visibleCards, 1);
    return;
  }

  if (evt.key === 'k') {
    evt.preventDefault();
    moveFocus(visibleCards, -1);
    return;
  }

  if (evt.key === 'Enter') {
    const focused = document.querySelector('.proxy.focused');
    if (focused) {
      const openLink = focused.querySelector('.openbtn a');
      if (openLink) openLink.click();
    }
    return;
  }

  if (evt.key === 'i') {
    const focused = document.querySelector('.proxy.focused');
    if (focused) {
      const modalBtn = focused.querySelector('.info-btn');
      if (modalBtn) modalBtn.click();
    }
    return;
  }
});

function getVisibleCards() {
  const list = document.getElementById('proxy-list');
  if (!list) return [];
  return [...list.querySelectorAll('.proxy')].filter((c) => {
    if (c.style.display === 'none') return false;
    const parent = c.closest('.proxy-group-items');
    if (parent && parent.classList.contains('collapsed')) return false;
    return true;
  });
}

function clearFocused() {
  const prev = document.querySelector('.proxy.focused');
  if (prev) prev.classList.remove('focused');
  focusedCardId = '';
}

function moveFocus(cards, direction) {
  let idx = cards.findIndex((c) => c.id === focusedCardId);
  clearFocused();
  if (idx === -1) {
    idx = direction > 0 ? 0 : cards.length - 1;
  } else {
    idx += direction;
  }
  if (idx < 0 || idx >= cards.length) return;
  const card = cards[idx];
  card.classList.add('focused');
  focusedCardId = card.id;
  card.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}
