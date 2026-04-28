<script>
  import { api } from '../lib/api.js';
  import UploadModal from '../components/UploadModal.svelte';
  import VideoPlayer from '../components/VideoPlayer.svelte';

  let videos = $state([]);
  let profiles = $state([]);
  let loading = $state(true);
  let error = $state('');
  let showUpload = $state(false);
  let playingVideo = $state(null);
  let togglingId = $state(null);
  let deletingId = $state(null);
  let downloadingId = $state(null);

  async function load() {
    loading = true;
    error = '';
    try {
      const [vRes, cRes] = await Promise.all([api.listVideos(), api.getConfig()]);
      videos = vRes.videos ?? [];
      profiles = cRes.profiles ?? [];
    } catch (err) {
      error = err.message;
    } finally {
      loading = false;
    }
  }

  async function toggleVisibility(video) {
    const next = video.visibility === 'public' ? 'private' : 'public';
    togglingId = video.public_id;
    try {
      await api.setVisibility(video.public_id, next);
      video.visibility = next;
    } catch (err) {
      error = err.message;
    } finally {
      togglingId = null;
    }
  }

  async function deleteVideo(video) {
    if (!window.confirm(`Delete video "${video.original_name.replace(/"/g, '\\"')}"? This cannot be undone.`)) return;
    deletingId = video.public_id;
    try {
      await api.deleteVideo(video.public_id);
      videos = videos.filter(v => v.public_id !== video.public_id);
    } catch (err) {
      error = err.message;
    } finally {
      deletingId = null;
    }
  }

  async function downloadVideo(video) {
    downloadingId = video.public_id;
    try {
      const { blob, filename } = await api.downloadVideo(video.public_id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      error = err.message;
    } finally {
      downloadingId = null;
    }
  }

  function onUploaded() {
    load();
  }

  $effect(() => { load(); });

  const statusLabel = { queued: 'Queued', processing: 'Processing', ready: 'Ready', failed: 'Failed' };
  const statusClass = { queued: 'badge-queued', processing: 'badge-processing', ready: 'badge-ready', failed: 'badge-failed' };
</script>

<div class="page">
  <div class="page-header">
    <h2>Videos</h2>
    <button class="btn-primary" onclick={() => { showUpload = true; }}>＋ Upload</button>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if loading}
    <p class="muted">Loading...</p>
  {:else if videos.length === 0}
    <p class="muted">No videos found.</p>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>File Name</th>
            <th>Status</th>
            <th>Visibility</th>
            <th>Created At</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each videos as v (v.public_id)}
            <tr>
              <td class="mono">{v.public_id}</td>
              <td>{v.original_name}</td>
              <td><span class="badge {statusClass[v.status] ?? ''}">{statusLabel[v.status] ?? v.status}</span></td>
              <td>
                <span class="badge {v.visibility === 'public' ? 'badge-public' : 'badge-private'}">
                  {v.visibility === 'public' ? 'Public' : 'Private'}
                </span>
              </td>
              <td class="mono">{v.created_at ?? ''}</td>
              <td class="actions-cell">
                <button
                  class="btn-small"
                  disabled={v.status !== 'ready'}
                  onclick={() => { playingVideo = v; }}
                >Play</button>
                <button
                  class="btn-small btn-toggle"
                  disabled={togglingId === v.public_id}
                  onclick={() => toggleVisibility(v)}
                >{v.visibility === 'public' ? 'Make Private' : 'Make Public'}</button>
                <button
                  class="btn-small btn-delete"
                  disabled={deletingId === v.public_id}
                  onclick={() => deleteVideo(v)}
                >Delete</button>
                <button
                  class="btn-small btn-download"
                  disabled={v.status !== 'ready' || downloadingId === v.public_id}
                  onclick={() => downloadVideo(v)}
                >{downloadingId === v.public_id ? 'Downloading…' : 'Download'}</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

{#if showUpload}
  <UploadModal onClose={() => { showUpload = false; }} {onUploaded} />
{/if}

{#if playingVideo}
  <VideoPlayer video={playingVideo} {profiles} onClose={() => { playingVideo = null; }} />
{/if}

<style>
  .page { padding: 1rem 1.5rem; }
  .page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
  h2 { margin: 0; }
  .btn-primary { padding: .5rem 1rem; background: #2563eb; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-primary:hover { background: #1d4ed8; }
  .table-wrap { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: .875rem; }
  th { text-align: left; padding: .5rem .75rem; background: #f3f4f6; border-bottom: 2px solid #e5e7eb; font-weight: 600; }
  td { padding: .5rem .75rem; border-bottom: 1px solid #e5e7eb; vertical-align: middle; }
  tr:hover td { background: #f9fafb; }
  .mono { font-family: monospace; font-size: .8rem; }
  .badge {
    display: inline-block; padding: .2rem .5rem; border-radius: 9999px;
    font-size: .75rem; font-weight: 600; white-space: nowrap;
  }
  .badge-queued { background: #fef3c7; color: #92400e; }
  .badge-processing { background: #dbeafe; color: #1e40af; }
  .badge-ready { background: #d1fae5; color: #065f46; }
  .badge-failed { background: #fee2e2; color: #991b1b; }
  .badge-public { background: #d1fae5; color: #065f46; }
  .badge-private { background: #f3f4f6; color: #6b7280; }
  .actions-cell { white-space: nowrap; display: flex; gap: .4rem; }
  .btn-small { padding: .25rem .6rem; font-size: .75rem; border: 1px solid #d1d5db; border-radius: 4px; cursor: pointer; background: white; }
  .btn-small:hover:not(:disabled) { background: #f3f4f6; }
  .btn-small:disabled { opacity: .4; cursor: not-allowed; }
  .btn-toggle { color: #374151; }
  .btn-delete { color: #dc2626; border-color: #fca5a5; }
  .btn-delete:hover:not(:disabled) { background: #fee2e2; }
  .btn-download { color: #2563eb; border-color: #93c5fd; }
  .btn-download:hover:not(:disabled) { background: #eff6ff; }
  .error { color: #dc2626; font-size: .875rem; }
  .muted { color: #9ca3af; }
</style>
