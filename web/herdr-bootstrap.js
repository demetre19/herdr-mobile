const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.10-387-7a941baad7726cbc/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
