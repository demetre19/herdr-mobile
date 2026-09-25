const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.3-387-de7e50c2b28e1d1b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
