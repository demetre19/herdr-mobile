const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.40-393-b5e0bd490609ca96/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
