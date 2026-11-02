const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-fb1d2d72563192a8/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
