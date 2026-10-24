const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.30-391-7404622b59c43627/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
