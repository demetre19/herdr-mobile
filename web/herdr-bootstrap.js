const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.45-393-d0c266b77f37e81c/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
