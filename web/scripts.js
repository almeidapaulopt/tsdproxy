import "./node_modules/datastar/bundles/datastar.js";


window.sortList = function() {
  const list = document.getElementById("proxy-list");
  if (!list) return;

  const items = [...list.children].sort((a, b) => {
    return a.id.localeCompare(b.id);
  });

  items.forEach(item => list.appendChild(item));
}


