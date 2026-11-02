const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.34-393-cf6870a84fbd64d2/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
