const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.29-388-7ca5958aa829e0f9/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
