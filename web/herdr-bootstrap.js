const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.2-387-ab222b69c6cff83c/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
