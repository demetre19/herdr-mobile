const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.53-393-de71e3e265a61eb5/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
