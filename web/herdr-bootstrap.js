const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.28-387-2229768ee0ba01e3/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
