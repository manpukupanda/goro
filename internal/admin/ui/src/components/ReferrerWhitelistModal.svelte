<script>
  import { api } from '../lib/api.js';

  const { video, onClose, onSaved } = $props();

  let inputText = $state((video.referrer_whitelist ?? []).join('\n'));
  let saving = $state(false);
  let error = $state('');

  async function handleSave() {
    const list = inputText
      .split(/\r?\n/)
      .map(v => v.trim())
      .filter(Boolean);

    saving = true;
    error = '';
    try {
      const res = await api.setReferrerWhitelist(video.public_id, list);
      onSaved(res.referrer_whitelist ?? []);
      onClose();
    } catch (err) {
      error = err.message;
    } finally {
      saving = false;
    }
  }

  function handleOverlayClick(e) {
    if (e.target === e.currentTarget && !saving) onClose();
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="overlay" onclick={handleOverlayClick}>
  <div class="modal">
    <div class="modal-header">
      <h2>Referrer Whitelist — {video.original_name}</h2>
      <button class="close-btn" onclick={onClose} disabled={saving}>✕</button>
    </div>

    <p class="hint">
      Enter one domain rule per line. Leave empty to allow all referrers.<br />
      Accepted formats: <code>example.com</code>, <code>*.example.com</code>, <code>example.com:8443</code>
    </p>

    <textarea
      class="rules-input"
      bind:value={inputText}
      disabled={saving}
      rows="8"
      placeholder="example.com&#10;*.example.com&#10;example.com:8443"
      spellcheck="false"
    ></textarea>

    {#if error}
      <p class="error">{error}</p>
    {/if}

    <div class="actions">
      <button class="btn-secondary" onclick={onClose} disabled={saving}>Cancel</button>
      <button class="btn-primary" onclick={handleSave} disabled={saving}>
        {saving ? 'Saving…' : 'Save'}
      </button>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,.5);
    display: flex; justify-content: center; align-items: center; z-index: 100;
  }
  .modal {
    background: white; border-radius: 8px; padding: 1.5rem; width: 480px;
    display: flex; flex-direction: column; gap: 1rem;
  }
  .modal-header {
    display: flex; align-items: flex-start; justify-content: space-between; gap: .5rem;
  }
  h2 {
    margin: 0; font-size: 1rem;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .close-btn {
    background: none; border: none; color: #6b7280;
    font-size: 1.25rem; cursor: pointer; padding: .25rem; flex-shrink: 0; line-height: 1;
  }
  .close-btn:hover:not(:disabled) { color: #111827; }
  .hint {
    margin: 0; font-size: .8rem; color: #6b7280; line-height: 1.5;
  }
  code { font-family: monospace; background: #f3f4f6; padding: .1em .3em; border-radius: 3px; }
  .rules-input {
    width: 100%; padding: .5rem .75rem; border: 1px solid #d1d5db; border-radius: 4px;
    font-family: monospace; font-size: .875rem; resize: vertical;
    box-sizing: border-box;
  }
  .rules-input:focus { outline: 2px solid #2563eb; outline-offset: -1px; border-color: transparent; }
  .rules-input:disabled { background: #f9fafb; color: #9ca3af; }
  .error { margin: 0; color: #dc2626; font-size: .875rem; }
  .actions { display: flex; gap: .5rem; justify-content: flex-end; }
  .btn-primary { padding: .5rem 1rem; background: #2563eb; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-primary:hover:not(:disabled) { background: #1d4ed8; }
  .btn-secondary { padding: .5rem 1rem; background: #f3f4f6; color: #374151; border: none; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-secondary:hover:not(:disabled) { background: #e5e7eb; }
  button:disabled { opacity: .5; cursor: not-allowed; }
</style>
