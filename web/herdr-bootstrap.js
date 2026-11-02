const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-050d09b6247fb5e4/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
