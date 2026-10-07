const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.12-387-b9076440b2971c2b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
