const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-fd92028d9a86f76f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
