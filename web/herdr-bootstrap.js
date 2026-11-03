const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.47-393-8bca57b892568df8/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
