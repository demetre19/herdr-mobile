const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.10-387-3eb622a805d39620/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
