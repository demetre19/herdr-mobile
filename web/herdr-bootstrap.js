const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.39-393-dd1db2aa7a638514/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
