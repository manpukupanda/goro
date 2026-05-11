<script>
  import ThumbnailImage from './ThumbnailImage.svelte';

  const { video, thumbnailSpecs, onClose } = $props();

  let enlarged = $state(null); // specName of the currently enlarged thumbnail
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="overlay" onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
  <div class="modal">
    <div class="modal-header">
      <h2>Thumbnails — {video.original_name}</h2>
      <button class="close-btn" onclick={onClose}>✕</button>
    </div>

    {#if enlarged}
      <!-- Enlarged single thumbnail view -->
      <div class="enlarged-wrap">
        <ThumbnailImage
          videoId={video.public_id}
          specName={enlarged}
          className="enlarged-img"
          alt={enlarged}
        />
        <p class="spec-label">{enlarged}</p>
        <button class="btn-back" onclick={() => { enlarged = null; }}>← Back to list</button>
      </div>
    {:else}
      <!-- Thumbnail grid -->
      {#if thumbnailSpecs.length === 0}
        <p class="muted">No thumbnail presets configured.</p>
      {:else}
        <div class="grid">
          {#each thumbnailSpecs as spec}
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
            <div
              class="thumb-card"
              onclick={() => { enlarged = spec; }}
              title="Click to enlarge"
            >
              <ThumbnailImage
                videoId={video.public_id}
                specName={spec}
                className="thumb-img"
                alt={spec}
              />
              <p class="spec-label">{spec}</p>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,.75);
    display: flex; justify-content: center; align-items: center; z-index: 200;
  }
  .modal {
    background: #1a1a1a; border-radius: 8px; padding: 1.25rem;
    width: 90vw; max-width: 720px; display: flex; flex-direction: column; gap: 1rem;
    max-height: 90vh; overflow-y: auto;
  }
  .modal-header {
    display: flex; align-items: center; justify-content: space-between;
  }
  h2 {
    margin: 0; font-size: 1rem; color: white;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .close-btn {
    background: none; border: none; color: #ccc;
    font-size: 1.25rem; cursor: pointer; padding: .25rem; flex-shrink: 0;
  }
  .close-btn:hover { color: white; }

  /* Grid */
  .grid {
    display: flex; flex-wrap: wrap; gap: .75rem;
  }
  .thumb-card {
    background: #2a2a2a; border-radius: 6px; padding: .5rem;
    cursor: pointer; text-align: center;
    transition: background .15s;
    flex: 1 1 180px; max-width: 220px;
  }
  .thumb-card:hover { background: #333; }

  /* Enlarged */
  .enlarged-wrap {
    display: flex; flex-direction: column; align-items: center; gap: .75rem;
  }
  .btn-back {
    background: none; border: 1px solid #555; color: #ccc;
    padding: .3rem .75rem; border-radius: 4px; cursor: pointer; font-size: .8rem;
  }
  .btn-back:hover { background: #2a2a2a; color: white; }

  .spec-label {
    margin: .35rem 0 0; color: #9ca3af; font-size: .75rem;
  }
  .muted { color: #6b7280; font-size: .875rem; margin: 0; }

  /* Applied to ThumbnailImage's img via className prop */
  :global(.thumb-img) {
    width: 100%; height: 120px; object-fit: cover; border-radius: 4px; display: block;
  }
  :global(.enlarged-img) {
    max-width: 100%; max-height: 60vh; object-fit: contain; border-radius: 4px;
  }
</style>
