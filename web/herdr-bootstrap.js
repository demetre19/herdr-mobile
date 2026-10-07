const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.14-387-0e5bcd7f0453414a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
