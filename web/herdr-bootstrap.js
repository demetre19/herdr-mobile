const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.4-387-02ea9919b1d31e1f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
