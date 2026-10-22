const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.22-387-9eafb088aebb4b66/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
