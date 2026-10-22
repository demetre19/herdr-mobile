const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.21-387-933e33b4cb660a2f/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
