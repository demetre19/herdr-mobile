const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.21-387-2cf25e5f8152a884/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
