const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-a75db76c5a9c3ee4/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
