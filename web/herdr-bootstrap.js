const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.41-393-944d5116fdb16b17/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
