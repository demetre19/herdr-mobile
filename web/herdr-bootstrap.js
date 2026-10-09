const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-792be00241124245/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
