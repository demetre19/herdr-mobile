const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.9-387-0f3b95032238ad80/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
