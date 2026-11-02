const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.36-393-8e3259f0cc798d58/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
