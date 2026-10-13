const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.17-387-45ca632dab4c186a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
