const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-2a11c936e1fce7d1/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
