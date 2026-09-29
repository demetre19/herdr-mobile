const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.8-387-e3bd3fe7dd263e2a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
