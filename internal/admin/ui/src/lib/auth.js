// Stores the pre-computed Basic Auth token (base64 of "user:pass") in
// sessionStorage. The raw password is never stored.
const KEY = 'goro_admin_auth';

export function isAuthenticated() {
  return sessionStorage.getItem(KEY) !== null;
}

export function setCredentials(username, password) {
  const token = btoa(`${username}:${password}`);
  sessionStorage.setItem(KEY, token);
}

export function clearCredentials() {
  sessionStorage.removeItem(KEY);
}

export function authHeader() {
  const token = sessionStorage.getItem(KEY);
  if (!token) return {};
  return { Authorization: `Basic ${token}` };
}
