const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.0-387-8cbfe252aa8a6776/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
