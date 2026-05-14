// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

import { updateState } from './state.js';

const logStreams = new Map();
let delegationBound = false;

function safeID(name) {
	let result = '';
	for (let i = 0; i < name.length; i++) {
		const cp = name.codePointAt(i);
		if (cp === undefined) continue;

		const ch = String.fromCodePoint(cp);
		if (
			(ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch === '-'
		) {
			result += ch;
		} else {
			result += '_' + cp.toString(16) + '_';
		}

		if (cp > 0xffff) i++;
	}
	return result;
}

function scrollToBottom(selector) {
	const el = document.querySelector(selector);
	if (el) el.scrollTop = el.scrollHeight;
}

function trimChildren(selector, max) {
	const el = document.querySelector(selector);
	if (!el) return;
	const limit = parseInt(max, 10);
	if (isNaN(limit)) return;
	while (el.children.length > limit) {
		el.removeChild(el.firstChild);
	}
}

function showNotification(id, status) {
	if (typeof window.showProxyNotification === 'function') {
		window.showProxyNotification(id, status);
	}
}

// ---------------------------------------------------------------------------
// Event delegation (set up once on #proxy-list)
// ---------------------------------------------------------------------------

function setupDelegation() {
	if (delegationBound) return;
	const proxyList = document.getElementById('proxy-list');
	if (!proxyList) return;
	delegationBound = true;

	proxyList.addEventListener('click', (e) => {
		const pinBtn = e.target.closest('.pin-btn');
		if (pinBtn) {
			const card = pinBtn.closest('.proxy');
			if (card) {
				window.togglePin(card.id);
				window.sortList();
			}
			return;
		}

		const infoBtn = e.target.closest('.info-btn');
		if (infoBtn) {
			const card = infoBtn.closest('.proxy');
			if (card) {
				const modal = document.getElementById(safeID(card.id) + '_modal');
				if (modal) modal.showModal();
			}
			return;
		}

		const copyBtn = e.target.closest('[data-copy]');
		if (copyBtn) {
			navigator.clipboard.writeText(copyBtn.dataset.copy).catch(() => {});
			return;
		}
	});

	proxyList.addEventListener('change', (e) => {
		const tab = e.target.closest('input.tab');
		if (!tab) return;
		const card = tab.closest('.proxy');
		if (!card) return;

		const sid = safeID(card.id);
		const logStreamEl = document.getElementById('log-stream-' + sid);
		if (logStreamEl) logStreamEl.innerHTML = '';

		if (tab.id.includes('-logs-tab-')) {
			connectLogStream(card.id);
		} else {
			disconnectLogStream(card.id);
		}
	});

	proxyList.addEventListener('close', (e) => {
		const dialog = e.target.closest('dialog.modal');
		if (!dialog) return;
		const card = dialog.closest('.proxy');
		if (!card) return;

		const sid = safeID(card.id);
		disconnectLogStream(card.id);

		const logStreamEl = document.getElementById('log-stream-' + sid);
		if (logStreamEl) logStreamEl.innerHTML = '';

		const infoTab = document.getElementById('log-info-tab-' + sid);
		if (infoTab) infoTab.checked = true;
	});
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export function connectSSE(url) {
	setupDelegation();
	const es = new EventSource(url);

	es.addEventListener('proxy-append', (e) => {
		const list = document.getElementById('proxy-list');
		if (!list) return;
		list.insertAdjacentHTML('beforeend', e.data);
	});

	es.addEventListener('proxy-merge', (e) => {
		const doc = new DOMParser().parseFromString(e.data, 'text/html');
		const newCard = doc.querySelector('.proxy');
		if (!newCard) return;

		const cardId = newCard.id;
		const existingCard = document.getElementById(cardId);
		if (!existingCard) return;

		const openDialog = existingCard.querySelector('dialog[open]');
		if (openDialog) {
			const oldBadge = existingCard.querySelector('.proxy-status-row .badge');
			const newBadge = newCard.querySelector('.proxy-status-row .badge');
			if (oldBadge && newBadge) {
				oldBadge.className = newBadge.className;
				oldBadge.textContent = newBadge.textContent;
			}

			const oldHealth = existingCard.querySelector('.health-dot');
			const newHealth = newCard.querySelector('.health-dot');
			if (oldHealth && newHealth) {
				oldHealth.className = newHealth.className;
				oldHealth.title = newHealth.title;
			} else if (newHealth && !oldHealth) {
				const statusRow = existingCard.querySelector('.proxy-status-row');
				if (statusRow) statusRow.appendChild(newHealth.cloneNode(true));
			} else if (!newHealth && oldHealth) {
				oldHealth.remove();
			}

			const oldURL = existingCard.querySelector('.openbtn a');
			const newURL = newCard.querySelector('.openbtn a');
			if (oldURL && newURL) {
				oldURL.href = newURL.href;
				oldURL.textContent = newURL.textContent;
				oldURL.className = newURL.className;
			}
		} else {
			existingCard.outerHTML = e.data;
		}
	});

	es.addEventListener('proxy-remove', (e) => {
		const el = document.querySelector(e.data);
		if (el) el.remove();
	});

	es.addEventListener('list-clear', (e) => {
		const el = document.querySelector(e.data);
		if (el) el.innerHTML = '';
	});

	es.addEventListener('sort-list', () => {
		if (typeof window.sortList === 'function') window.sortList();
	});

	es.addEventListener('notify', (e) => {
		const sep = e.data.indexOf('\x00');
		if (sep === -1) return;
		showNotification(e.data.slice(0, sep), e.data.slice(sep + 1));
	});

	es.addEventListener('update-state', (e) => {
		updateState(e.data);
	});

	return es;
}

export function connectLogStream(proxyName) {
	disconnectLogStream(proxyName);

	const encoded = encodeURIComponent(proxyName);
	const url = '/stream/' + encoded + '/logs';
	const sid = safeID(proxyName);
	const containerSelector = '#log-lines-' + sid;

	const es = new EventSource(url);
	logStreams.set(proxyName, es);

	es.addEventListener('proxy-append', (e) => {
		const container = document.querySelector(containerSelector);
		if (container) {
			container.insertAdjacentHTML('beforeend', e.data);
		}
	});

	es.addEventListener('proxy-remove', (e) => {
		const el = document.querySelector(e.data);
		if (el) el.remove();
	});

	es.addEventListener('list-clear', (e) => {
		const el = document.querySelector(e.data);
		if (el) el.innerHTML = '';
	});

	es.addEventListener('scroll-logs', (e) => {
		scrollToBottom(e.data);
	});

	es.addEventListener('trim-logs', (e) => {
		const lines = e.data.split('\n');
		trimChildren(lines[0], lines[1]);
	});

	return es;
}

export function disconnectLogStream(proxyName) {
	const es = logStreams.get(proxyName);
	if (es) {
		es.close();
		logStreams.delete(proxyName);
	}
}

export function bindCardEvents(_element) {
	setupDelegation();
}
