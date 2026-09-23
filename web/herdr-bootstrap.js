const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.0-387-b9fc5ea2b217f9ea/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
