const e = new URL(window.__HERDR_ENTRY__ || "/builds/1.0.19-387-a48704f25eccc290/index.html", location);
  e.search = location.search;
  e.hash = location.hash;
  location.replace(e);
