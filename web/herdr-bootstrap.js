const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-3dc279bb62b2c06d/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
