const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-c5a538b460b318fd/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
