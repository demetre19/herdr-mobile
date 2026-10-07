const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-7441427a0b1fbd52/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
