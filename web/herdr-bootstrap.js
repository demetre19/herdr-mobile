const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.12-387-2b14ddba5142e3f6/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
