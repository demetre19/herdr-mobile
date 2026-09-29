const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.6-387-1bf3aa20d17deacf/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
