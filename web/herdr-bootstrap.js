const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-65dd2ed8e78e3dbf/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
