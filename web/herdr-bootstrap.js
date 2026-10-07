const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.15-387-2ccd9526e08ebe9b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
