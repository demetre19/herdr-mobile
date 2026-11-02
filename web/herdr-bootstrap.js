const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-105e7d49e1c72cdf/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
