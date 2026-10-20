const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.20-387-3902e832445df149/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
