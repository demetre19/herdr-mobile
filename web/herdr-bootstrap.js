const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-b4c0c7eb4fec5e8d/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
