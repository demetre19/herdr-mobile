const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.38-393-fe4bc63b4ef5d628/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
