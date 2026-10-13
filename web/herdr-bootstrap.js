const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.18-387-71c9f3db8b7f421a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
