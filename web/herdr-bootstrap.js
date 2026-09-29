const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.6-387-fa2c3b5684d49e63/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
