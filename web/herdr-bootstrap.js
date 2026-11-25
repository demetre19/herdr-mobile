const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.58-393-e9a5cb3a3b6e4ce5/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
