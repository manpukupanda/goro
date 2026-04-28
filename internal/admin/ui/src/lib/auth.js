// Stores Basic Auth credentials in sessionStorage.
const KEY = 'goro_admin_auth';

export function getCredentials() {
  const raw = sessionStorage.getItem(KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function setCredentials(username, password) {
  sessionStorage.setItem(KEY, JSON.stringify({ username, password }));
}

export function clearCredentials() {
  sessionStorage.removeItem(KEY);
}

export function authHeader() {
  const creds = getCredentials();
  if (!creds) return {};
  const encoded = btoa(`${creds.username}:${creds.password}`);
  return { Authorization: `Basic ${encoded}` };
}
