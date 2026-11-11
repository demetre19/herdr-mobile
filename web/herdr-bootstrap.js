const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.54-393-566d95535cd4b58a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
