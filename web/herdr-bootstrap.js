const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-87c1f651111de147/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
