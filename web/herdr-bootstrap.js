const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.0-387-2d030335c720185f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
