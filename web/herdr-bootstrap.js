const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-0c73f98287be1dfe/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
