import "./node_modules/datastar/bundles/datastar.js";


window.sortList = function() {
  const list = document.getElementById("proxy-list");
  if (!list) return;

  const items = [...list.children].sort((a, b) => {
    return a.id.localeCompare(b.id);
  });

  items.forEach(item => list.appendChild(item));
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
