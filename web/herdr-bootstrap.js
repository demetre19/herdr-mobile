const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.5-387-3108152e2beba752/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
