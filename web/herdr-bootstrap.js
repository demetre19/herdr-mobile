const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.16-387-d5dd11ff6805c25d/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
