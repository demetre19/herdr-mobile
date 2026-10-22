const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.23-387-c312ef6696469d89/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
