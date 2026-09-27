const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.5-387-2ac284824122276b/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
