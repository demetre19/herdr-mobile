const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.27-387-bbe461bcc5a694ec/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
