<script>
  import { api } from '../lib/api.js';

  const { onClose, onUploaded } = $props();

  let file = $state(null);
  let progress = $state(0);
  let uploading = $state(false);
  let error = $state('');

  function onFileChange(e) {
    file = e.target.files[0] || null;
    error = '';
  }

  async function handleUpload() {
    if (!file) { error = 'ファイルを選択してください'; return; }
    if (!file.name.toLowerCase().endsWith('.mp4')) { error = '.mp4 ファイルのみアップロードできます'; return; }
    uploading = true;
    error = '';
    try {
      const result = await api.uploadVideo(file, (p) => { progress = p; });
      onUploaded(result.video_id);
      onClose();
    } catch (err) {
      error = err.message;
    } finally {
      uploading = false;
      progress = 0;
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="overlay" onclick={(e) => { if (e.target === e.currentTarget && !uploading) onClose(); }}>
  <div class="modal">
    <h2>動画アップロード</h2>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <label class="file-label">
      <input type="file" accept=".mp4,video/mp4" onchange={onFileChange} disabled={uploading} />
      {#if file}
        <span>{file.name} ({(file.size / 1024 / 1024).toFixed(1)} MB)</span>
      {:else}
        <span>MP4 ファイルを選択...</span>
      {/if}
    </label>
    {#if uploading}
      <div class="progress-bar">
        <div class="progress-fill" style="width: {progress}%"></div>
      </div>
      <p class="progress-text">{progress}% アップロード中...</p>
    {/if}
    <div class="actions">
      <button class="btn-secondary" onclick={onClose} disabled={uploading}>キャンセル</button>
      <button class="btn-primary" onclick={handleUpload} disabled={uploading || !file}>アップロード</button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,.5);
    display: flex; justify-content: center; align-items: center; z-index: 100;
  }
  .modal {
    background: white; border-radius: 8px; padding: 1.5rem; width: 420px;
    display: flex; flex-direction: column; gap: 1rem;
  }
  h2 { margin: 0; font-size: 1.1rem; }
  .file-label {
    display: block; padding: .75rem 1rem; border: 2px dashed #ccc; border-radius: 6px;
    cursor: pointer; text-align: center; font-size: .875rem; color: #555;
  }
  .file-label input { display: none; }
  .progress-bar {
    height: 8px; background: #e5e7eb; border-radius: 4px; overflow: hidden;
  }
  .progress-fill { height: 100%; background: #2563eb; transition: width .2s; }
  .progress-text { font-size: .8rem; color: #555; margin: 0; text-align: center; }
  .actions { display: flex; gap: .5rem; justify-content: flex-end; }
  .btn-primary { padding: .5rem 1rem; background: #2563eb; color: white; border: none; border-radius: 4px; cursor: pointer; }
  .btn-primary:hover:not(:disabled) { background: #1d4ed8; }
  .btn-secondary { padding: .5rem 1rem; background: #f3f4f6; color: #374151; border: none; border-radius: 4px; cursor: pointer; }
  .btn-secondary:hover:not(:disabled) { background: #e5e7eb; }
  button:disabled { opacity: .5; cursor: not-allowed; }
  .error { color: #dc2626; font-size: .875rem; margin: 0; }
</style>
