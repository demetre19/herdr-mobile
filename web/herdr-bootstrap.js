const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.6-387-1ee77f782c7a09ef/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
