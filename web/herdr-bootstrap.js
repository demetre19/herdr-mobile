const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.26-387-427139e73fdc8da6/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
