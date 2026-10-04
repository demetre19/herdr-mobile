const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-25ae61baed48be35/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
