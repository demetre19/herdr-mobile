const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.13-387-79e249491be9fdb7/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
