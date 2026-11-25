const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.57-393-6f6024db36eab47e/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
