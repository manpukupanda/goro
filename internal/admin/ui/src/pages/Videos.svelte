<script>
  import { api } from '../lib/api.js';
  import UploadModal from '../components/UploadModal.svelte';
  import VideoPlayer from '../components/VideoPlayer.svelte';
  import ThumbnailImage from '../components/ThumbnailImage.svelte';
  import ThumbnailModal from '../components/ThumbnailModal.svelte';

  let videos = $state([]);
  let profiles = $state([]);
  let thumbnailSpecs = $state([]);
  let loading = $state(true);
  let error = $state('');
  let showUpload = $state(false);
  let playingVideo = $state(null);
  let thumbnailVideo = $state(null);
  let togglingId = $state(null);
  let deletingId = $state(null);
  let downloadingId = $state(null);

  // Filter state
  let filterName = $state('');
  let filterStatus = $state('');
  let filterVisibility = $state('');
  let filterCodec = $state('');
  let filterDurationMin = $state('');
  let filterDurationMax = $state('');
  let filterWidthMin = $state('');
  let filterWidthMax = $state('');
  let filterHeightMin = $state('');
  let filterHeightMax = $state('');

  function buildFilterParams() {
    return {
      name: filterName,
      status: filterStatus,
      visibility: filterVisibility,
      codec: filterCodec,
      duration_min: filterDurationMin,
      duration_max: filterDurationMax,
      width_min: filterWidthMin,
      width_max: filterWidthMax,
      height_min: filterHeightMin,
      height_max: filterHeightMax,
    };
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const [vRes, cRes] = await Promise.all([api.listVideos(buildFilterParams()), api.getConfig()]);
      videos = vRes.videos ?? [];
      profiles = cRes.profiles ?? [];
      thumbnailSpecs = cRes.thumbnail_specs ?? [];
    } catch (err) {
      error = err.message;
    } finally {
      loading = false;
    }
  }

  function applyFilters() {
    load();
  }

  function resetFilters() {
    filterName = '';
    filterStatus = '';
    filterVisibility = '';
    filterCodec = '';
    filterDurationMin = '';
    filterDurationMax = '';
    filterWidthMin = '';
    filterWidthMax = '';
    filterHeightMin = '';
    filterHeightMax = '';
    load();
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
      setTimeout(() => URL.revokeObjectURL(url), 100);
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

  /** Format seconds as mm:ss or h:mm:ss. */
  function formatDuration(sec) {
    if (sec == null) return '—';
    const s = Math.round(sec);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const ss = String(s % 60).padStart(2, '0');
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${ss}`;
    return `${m}:${ss}`;
  }

  /** Format bit/s as Mbps or kbps. */
  function formatBitrate(bps) {
    if (bps == null) return '—';
    if (bps >= 1_000_000) return (bps / 1_000_000).toFixed(1) + ' Mbps';
    return Math.round(bps / 1000) + ' kbps';
  }

  /** Format framerate float to two decimals. */
  function formatFps(fps) {
    if (fps == null) return '—';
    return fps.toFixed(2) + ' fps';
  }
</script>

<div class="page">
  <div class="page-header">
    <h2>Videos</h2>
    <button class="btn-primary" onclick={() => { showUpload = true; }}>＋ Upload</button>
  </div>

  <!-- Filter bar -->
  <details class="filter-box">
    <summary class="filter-summary">Filters</summary>
    <div class="filter-grid">
      <label>
        File name
        <input type="text" bind:value={filterName} placeholder="partial match" />
      </label>
      <label>
        Status
        <select bind:value={filterStatus}>
          <option value="">All</option>
          <option value="queued">Queued</option>
          <option value="processing">Processing</option>
          <option value="ready">Ready</option>
          <option value="failed">Failed</option>
        </select>
      </label>
      <label>
        Visibility
        <select bind:value={filterVisibility}>
          <option value="">All</option>
          <option value="public">Public</option>
          <option value="private">Private</option>
        </select>
      </label>
      <label>
        Codec
        <input type="text" bind:value={filterCodec} placeholder="e.g. h264" />
      </label>
      <label>
        Duration min (s)
        <input type="number" min="0" step="any" bind:value={filterDurationMin} placeholder="0" />
      </label>
      <label>
        Duration max (s)
        <input type="number" min="0" step="any" bind:value={filterDurationMax} placeholder="∞" />
      </label>
      <label>
        Width min (px)
        <input type="number" min="0" step="1" bind:value={filterWidthMin} placeholder="0" />
      </label>
      <label>
        Width max (px)
        <input type="number" min="0" step="1" bind:value={filterWidthMax} placeholder="∞" />
      </label>
      <label>
        Height min (px)
        <input type="number" min="0" step="1" bind:value={filterHeightMin} placeholder="0" />
      </label>
      <label>
        Height max (px)
        <input type="number" min="0" step="1" bind:value={filterHeightMax} placeholder="∞" />
      </label>
    </div>
    <div class="filter-actions">
      <button class="btn-primary" onclick={applyFilters}>Apply</button>
      <button class="btn-secondary" onclick={resetFilters}>Reset</button>
    </div>
  </details>

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
            <th>Thumbnail</th>
            <th>Duration</th>
            <th>Resolution</th>
            <th>Codec</th>
            <th>Bitrate</th>
            <th>FPS</th>
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
              <td class="thumb-cell">
                {#if v.status === 'ready' && thumbnailSpecs.length > 0}
                  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
                  <div class="thumb-preview" onclick={() => { thumbnailVideo = v; }} title="クリックで拡大">
                    <ThumbnailImage
                      videoId={v.public_id}
                      specName={thumbnailSpecs[0]}
                      className="thumb-table-img"
                      alt="thumbnail"
                    />
                  </div>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
              <td class="mono">{formatDuration(v.duration_sec)}</td>
              <td class="mono">{v.width != null && v.height != null ? `${v.width}×${v.height}` : '—'}</td>
              <td class="mono">{v.video_codec ?? '—'}</td>
              <td class="mono">{formatBitrate(v.bitrate)}</td>
              <td class="mono">{formatFps(v.framerate_float)}</td>
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

{#if thumbnailVideo}
  <ThumbnailModal video={thumbnailVideo} {thumbnailSpecs} onClose={() => { thumbnailVideo = null; }} />
{/if}

<style>
  .page { padding: 1rem 1.5rem; }
  .page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
  h2 { margin: 0; }
  .btn-primary { padding: .5rem 1rem; background: #2563eb; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-primary:hover { background: #1d4ed8; }
  .btn-secondary { padding: .4rem .8rem; background: #f3f4f6; color: #374151; border: 1px solid #d1d5db; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-secondary:hover { background: #e5e7eb; }

  /* Filter bar */
  .filter-box { margin-bottom: 1rem; border: 1px solid #e5e7eb; border-radius: 6px; padding: .75rem 1rem; background: #fafafa; }
  .filter-summary { cursor: pointer; font-weight: 600; font-size: .875rem; color: #374151; user-select: none; }
  .filter-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: .6rem;
    margin-top: .75rem;
  }
  .filter-grid label { display: flex; flex-direction: column; font-size: .8rem; color: #374151; gap: .2rem; }
  .filter-grid input,
  .filter-grid select { padding: .3rem .5rem; border: 1px solid #d1d5db; border-radius: 4px; font-size: .8rem; }
  .filter-actions { display: flex; gap: .5rem; margin-top: .75rem; }

  .table-wrap { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: .875rem; }
  th { text-align: left; padding: .5rem .75rem; background: #f3f4f6; border-bottom: 2px solid #e5e7eb; font-weight: 600; white-space: nowrap; }
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
  .thumb-cell { vertical-align: middle; }
  .thumb-preview { cursor: pointer; display: inline-block; }
  :global(.thumb-table-img) {
    width: 80px; height: 50px; object-fit: cover; border-radius: 3px; display: block;
  }
  .error { color: #dc2626; font-size: .875rem; }
  .muted { color: #9ca3af; }
</style>

