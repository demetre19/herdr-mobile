const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.24-387-c603ceb11bff715b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
