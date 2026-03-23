function trimLeadingSlashes(value) {
  return String(value || '').replace(/^\/+/, '');
}

function joinRelative(prefix, value) {
  const clean = trimLeadingSlashes(value);
  if (!clean) return `./${prefix}`;
  return `./${prefix}/${clean}`;
}

function currentBaseURL() {
  return new URL(document.baseURI || window.location.href);
}

function isStaticPage(url) {
  const pathname = String(url?.pathname || '');
  return pathname === '/static'
    || pathname.endsWith('/static')
    || pathname.includes('/static/');
}

export function appURL(path) {
  return new URL(String(path || './'), currentBaseURL()).toString();
}

export function apiURL(path) {
  return appURL(joinRelative('api', path));
}

export function wsURL(path) {
  const url = new URL(joinRelative('ws', path), document.baseURI || window.location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export function staticURL(path) {
  if (isStaticPage(currentBaseURL())) {
    const clean = trimLeadingSlashes(path);
    return appURL(clean ? `./${clean}` : './');
  }
  return appURL(joinRelative('static', path));
}
