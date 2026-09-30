const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.10-387-f2580e5ef1c668d4/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
