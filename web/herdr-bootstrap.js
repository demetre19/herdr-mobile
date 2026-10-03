const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-3191c0af2bcd57bc/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
