const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.55-393-da438698de7298ac/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
