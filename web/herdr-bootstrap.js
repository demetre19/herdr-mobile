const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.37-393-993266bea007e55e/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
