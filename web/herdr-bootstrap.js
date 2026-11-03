const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.42-393-9a3ad08af4747dd4/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
