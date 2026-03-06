function trimLeadingSlashes(value) {
  return String(value || '').replace(/^\/+/, '');
}

function joinRelative(prefix, value) {
  const clean = trimLeadingSlashes(value);
  if (!clean) return `./${prefix}`;
  return `./${prefix}/${clean}`;
}

export function appURL(path) {
  return new URL(String(path || './'), window.location.href).toString();
}

export function apiURL(path) {
  return appURL(joinRelative('api', path));
}

export function wsURL(path) {
  const url = new URL(joinRelative('ws', path), window.location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export function staticURL(path) {
  return appURL(joinRelative('static', path));
}
