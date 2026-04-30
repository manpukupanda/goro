<script>
  import { authHeader } from '../lib/auth.js';

  const BASE = '/admin/api';

  const { videoId, specName, className = '', alt = specName } = $props();

  let src = $state(null);
  let loading = $state(true);

  $effect(() => {
    // Re-run when videoId or specName change
    const _id = videoId;
    const _name = specName;

    loading = true;
    let objectUrl = null;

    fetch(`${BASE}/videos/${_id}/thumbnails/${encodeURIComponent(_name)}`, {
      headers: authHeader(),
    })
      .then((res) => (res.ok ? res.blob() : null))
      .then((blob) => {
        if (blob) {
          objectUrl = URL.createObjectURL(blob);
          src = objectUrl;
        }
      })
      .catch(() => {})
      .finally(() => {
        loading = false;
      });

    return () => {
      if (objectUrl) URL.revokeObjectURL(objectUrl);
      src = null;
    };
  });
</script>

{#if src}
  <img {src} {alt} class={className} />
{:else if loading}
  <span class="thumb-loading">…</span>
{:else}
  <span class="thumb-missing">—</span>
{/if}

<style>
  .thumb-loading,
  .thumb-missing {
    display: inline-block;
    color: #9ca3af;
    font-size: .75rem;
  }
</style>
