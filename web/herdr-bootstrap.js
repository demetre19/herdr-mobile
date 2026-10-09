const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-0658357cbc4bf5d2/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
