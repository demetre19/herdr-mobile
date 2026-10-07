const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.12-387-51ae95faa9ee204c/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
