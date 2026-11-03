const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.44-393-93947e59a258afef/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
