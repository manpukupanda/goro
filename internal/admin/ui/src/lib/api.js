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
  listVideos(params = {}) {
    const qs = new URLSearchParams();
    for (const [key, val] of Object.entries(params)) {
      if (val !== '' && val !== null && val !== undefined) {
        qs.append(key, String(val));
      }
    }
    const query = qs.toString();
    return request('GET', `/videos${query ? '?' + query : ''}`);
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
  setReferrerWhitelist(id, referrer_whitelist) {
    return request('PUT', `/videos/${id}/referrer-whitelist`, { referrer_whitelist });
  },
  getStreamURL(id, profile, format) {
    const path = format === 'dash_fmp4' ? 'manifest' : 'playlist';
    return `${BASE}/videos/${id}/${path}?profile=${encodeURIComponent(profile)}`;
  },
  async downloadVideo(id) {
    const headers = { ...authHeader() };
    const res = await fetch(`${BASE}/videos/${id}/download`, { headers });
    if (res.status === 401) {
      clearCredentials();
      location.reload();
      throw new Error('Unauthorized');
    }
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`HTTP ${res.status}: ${text}`);
    }
    const blob = await res.blob();
    let filename = `video-${id}.mp4`;
    const cd = res.headers.get('content-disposition');
    if (cd) {
      const mStar = cd.match(/filename\*=UTF-8''([^;\s]+)/i);
      if (mStar) {
        filename = decodeURIComponent(mStar[1]);
      } else {
        const m = cd.match(/filename="([^"]+)"/);
        if (m) filename = m[1];
      }
    }
    return { blob, filename };
  },

  // Jobs
  listJobs() {
    return request('GET', '/jobs');
  },

  // Config
  getConfig() {
    return request('GET', '/config');
  },

  // Thumbnails
  async getThumbnailBlobURL(id, name) {
    const headers = { ...authHeader() };
    const res = await fetch(`${BASE}/videos/${id}/thumbnails/${encodeURIComponent(name)}`, { headers });
    if (!res.ok) return null;
    return URL.createObjectURL(await res.blob());
  },
};
