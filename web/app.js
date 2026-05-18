// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

// Minimal JS for the htmx dashboard: keyboard nav, notifications, toasts.
// All sort/filter/group/pin/rendering is server-side.

import 'htmx.org';
import 'htmx.org/dist/ext/hx-sse';

window.htmx = htmx;

// import('htmx.org/dist/ext/hx-sse').then(() => htmx.process(document.body));

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
  const el = document.getElementById(safeProxyId(name));
  const label = el ? el.querySelector('.card-title span')?.textContent : name;
  new Notification('TSDProxy: ' + label, {
    body: 'Status changed to ' + status,
    icon: '/icons/tsdproxy.svg',
  });
};

function safeProxyId(name) {
  let result = 'proxy-';
  for (const ch of name) {
    if (/[a-zA-Z0-9-]/.test(ch)) {
      result += ch;
    } else {
      result += '_' + ch.codePointAt(0).toString(16) + '_';
    }
  }
  return result;
}

window.handleConnId = function (evt) {
  const connId = typeof evt.detail === 'string' ? evt.detail : (evt.detail?.data || '');
  const el = document.getElementById('sseConnId');
  if (el) el.value = connId;
};

window.scrollLogs = function (evt) {
  const sel = typeof evt.detail === 'string' ? evt.detail : (evt.detail?.data || '');
  const el = document.querySelector(sel);
  if (el) el.scrollTop = el.scrollHeight;
};

window.trimLogs = function (evt) {
  const d = typeof evt.detail === 'string' ? evt.detail : (evt.detail?.data || '');
  const lines = d.split('\n');
  if (lines.length < 2) return;
  const el = document.querySelector(lines[0]);
  const lim = parseInt(lines[1], 10);
  if (el && !isNaN(lim)) while (el.children.length > lim) el.removeChild(el.firstChild);
};

// Clean up modal when dialog closes (native <dialog> close or Escape key).
document.addEventListener('close', (e) => {
  if (e.target.tagName === 'DIALOG' && e.target.closest('#modal-root')) {
    document.getElementById('modal-root').innerHTML = '';
  }
});

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
