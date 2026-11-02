const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.33-393-2a0911b99146a21a/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
