const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.25-387-42e1cf8ea845266e/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
