const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.7-387-07cf998e814a461f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
