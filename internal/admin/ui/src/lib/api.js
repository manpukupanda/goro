import { authHeader, clearCredentials } from './auth.js';

const BASE = '/admin/api';

async function request(method, path, body, isFormData) {
  const headers = { ...authHeader() };
  let bodyPayload;

  if (isFormData) {
    bodyPayload = body; // FormData — don't set Content-Type
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json';
    bodyPayload = JSON.stringify(body);
  }

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: bodyPayload,
  });

  if (res.status === 401) {
    clearCredentials();
    location.reload();
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const text = await res.text();
    throw new Error(`HTTP ${res.status}: ${text}`);
  }

  const ct = res.headers.get('content-type') || '';
  if (ct.includes('application/json')) {
    return res.json();
  }
  return res.text();
}

export const api = {
  // Videos
  listVideos() {
    return request('GET', '/videos');
  },
  uploadVideo(file, onProgress) {
    return new Promise((resolve, reject) => {
      const fd = new FormData();
      fd.append('file', file);
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `${BASE}/videos`);
      const headers = authHeader();
      for (const [k, v] of Object.entries(headers)) {
        xhr.setRequestHeader(k, v);
      }
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
      };
      xhr.onload = () => {
        if (xhr.status === 202) {
          resolve(JSON.parse(xhr.responseText));
        } else if (xhr.status === 401) {
          clearCredentials();
          location.reload();
          reject(new Error('Unauthorized'));
        } else {
          reject(new Error(`HTTP ${xhr.status}: ${xhr.responseText}`));
        }
      };
      xhr.onerror = () => reject(new Error('Network error'));
      xhr.send(fd);
    });
  },
  deleteVideo(id) {
    return request('DELETE', `/videos/${id}`);
  },
  setVisibility(id, visibility) {
    return request('PUT', `/videos/${id}/visibility`, { visibility });
  },
  getPlaylistURL(id, profile) {
    return `${BASE}/videos/${id}/playlist?profile=${encodeURIComponent(profile)}`;
  },
  getDownloadURL(id) {
    return `${BASE}/videos/${id}/download`;
  },

  // Jobs
  listJobs() {
    return request('GET', '/jobs');
  },

  // Config
  getConfig() {
    return request('GET', '/config');
  },
};
