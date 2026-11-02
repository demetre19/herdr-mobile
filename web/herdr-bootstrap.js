const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.35-393-96891629884fe769/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
