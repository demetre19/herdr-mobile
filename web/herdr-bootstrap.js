const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.50-393-652812f0ee2cd47b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
