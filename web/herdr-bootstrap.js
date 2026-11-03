const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.43-393-92b2980ecd9dcd44/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
