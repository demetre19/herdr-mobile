const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.25-387-598f4abf75e312a3/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
