const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.1-387-18553cca68261545/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
