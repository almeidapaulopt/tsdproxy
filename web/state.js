// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

/**
 * Simple reactive state management module.
 * Replaces Datastar's signal system. No external dependencies.
 */

const PERSIST_KEYS = new Set(["dark", "view", "sort", "grouped", "pinned"]);

const listeners = new Map();

const state = {
  dark: localStorage.getItem("dark") === "true",
  view: localStorage.getItem("view") || "card",
  sort: localStorage.getItem("sort") || "name",
  grouped: localStorage.getItem("grouped") === "true",
  search: "",
  filterStatus: "all",
  filterHealth: "all",
  pinned: localStorage.getItem("pinned") || "",
  focused: "",
  user_username: "",
  user_displayName: "",
  user_profilePicUrl: "",
};

/**
 * Update a single state key, persist if applicable, notify listeners.
 * @param {string} key
 * @param {*} value
 */
function setState(key, value) {
  const prev = state[key];
  state[key] = value;

  if (PERSIST_KEYS.has(key)) {
    localStorage.setItem(key, String(value));
  }

  const cbs = listeners.get(key);
  if (cbs) {
    for (const cb of cbs) {
      cb(value, prev);
    }
  }
}

/**
 * Register a change listener for a given state key.
 * @param {string} key
 * @param {(value: *, prev: *) => void} callback
 * @returns {() => void} unsubscribe function
 */
function onStateChange(key, callback) {
  if (!listeners.has(key)) {
    listeners.set(key, new Set());
  }
  listeners.get(key).add(callback);
  return () => {
    listeners.get(key)?.delete(callback);
  };
}

/**
 * Merge a JSON string (from SSE) into state, triggering listeners per key.
 * @param {string} jsonString
 */
function updateState(jsonString) {
  let data;
  try {
    data = JSON.parse(jsonString);
  } catch {
    return;
  }
  if (typeof data !== "object" || data === null) return;
  for (const key of Object.keys(data)) {
    setState(key, data[key]);
  }
}

export { state, setState, onStateChange, updateState };
