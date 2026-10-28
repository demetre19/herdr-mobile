const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.31-393-4f5519915a69903f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
