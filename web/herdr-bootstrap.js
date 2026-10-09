const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-1d02f9ba45ac608e/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
