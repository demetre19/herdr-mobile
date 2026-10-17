const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.20-387-6c970a74c4a2a95b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
