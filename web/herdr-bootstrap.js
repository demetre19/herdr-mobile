const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.49-393-5f7b3575426af54f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
