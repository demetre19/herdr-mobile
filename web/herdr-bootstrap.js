const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.11-387-65e97bb09a348ec0/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
