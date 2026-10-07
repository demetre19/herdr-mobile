const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-1dae7c403d9f11be/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
