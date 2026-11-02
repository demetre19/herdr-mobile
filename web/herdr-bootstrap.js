const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.32-393-c2cbc4e1dae17a86/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
