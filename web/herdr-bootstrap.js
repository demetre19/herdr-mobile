const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.51-393-8bf3fc8a6e1d2eb1/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
