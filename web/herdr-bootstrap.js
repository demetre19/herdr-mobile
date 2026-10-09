const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-7d9f7aabd39c7f39/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
