<script>
  import { onDestroy } from 'svelte';
  import Hls from 'hls.js';
  import { api } from '../lib/api.js';
  import { authHeader } from '../lib/auth.js';

  const { video, profiles, onClose } = $props();

  let selectedProfile = $state(profiles[0]?.name ?? '');
  let videoEl = $state(null);
  let hls = null;
  let dash = null;
  let error = $state('');

  function selectedProfileConfig(profileName) {
    return profiles.find((profile) => profile.name === profileName);
  }

  function destroyPlayers() {
    if (hls) { hls.destroy(); hls = null; }
    if (dash) { dash.reset(); dash = null; }
  }

  async function loadVideo(profileName) {
    if (!videoEl) return;
    error = '';
    destroyPlayers();

    const profile = selectedProfileConfig(profileName);
    if (!profile) {
      error = 'プロファイル設定が見つかりません';
      return;
    }

    const src = api.getStreamURL(video.public_id, profile.name, profile.format);
    if (profile.format === 'dash_fmp4') {
      import('dashjs').then(({ default: dashjs }) => {
        if (!videoEl) return;
        dash = dashjs.MediaPlayer().create();
        dash.extend('RequestModifier', () => ({
          modifyRequestURL(url) {
            return url;
          },
          modifyRequestHeader(xhr) {
            const headers = authHeader();
            for (const [key, value] of Object.entries(headers)) {
              xhr.setRequestHeader(key, value);
            }
            return xhr;
          },
        }), true);
        dash.on(dashjs.MediaPlayer.events.ERROR, (evt) => {
          error = `DASH エラー: ${evt?.error?.message ?? '再生に失敗しました'}`;
        });
        dash.initialize(videoEl, src, true);
      }).catch((err) => {
        error = `DASH プレイヤーの初期化に失敗しました: ${err.message}`;
      });
      return;
    }

    if (Hls.isSupported()) {
      hls = new Hls({
        xhrSetup(xhr) {
          const h = authHeader();
          if (h.Authorization) xhr.setRequestHeader('Authorization', h.Authorization);
        },
      });
      hls.loadSource(src);
      hls.attachMedia(videoEl);
      hls.on(Hls.Events.ERROR, (_evt, data) => {
        if (data.fatal) {
          error = `HLS エラー: ${data.type} / ${data.details}`;
        }
      });
    } else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
      videoEl.src = src;
    } else {
      error = 'このブラウザは HLS 再生に対応していません';
    }
  }

  $effect(() => {
    if (videoEl && selectedProfile) {
      loadVideo(selectedProfile);
    }
  });

  onDestroy(() => {
    destroyPlayers();
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div class="overlay" onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
  <div class="modal">
    <div class="modal-header">
      <h2>{video.original_name}</h2>
      <button class="close-btn" onclick={onClose}>✕</button>
    </div>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <div class="profile-select">
      <label>プロファイル:
        <select bind:value={selectedProfile} onchange={() => loadVideo(selectedProfile)}>
          {#each profiles as p}
            <option value={p.name}>{p.name}</option>
          {/each}
        </select>
      </label>
    </div>
    <div class="player-wrap">
      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={videoEl} controls autoplay class="video-player"></video>
    </div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,.7);
    display: flex; justify-content: center; align-items: center; z-index: 100;
  }
  .modal {
    background: #1a1a1a; border-radius: 8px; padding: 1rem; width: 90vw; max-width: 900px;
    display: flex; flex-direction: column; gap: .75rem;
  }
  .modal-header { display: flex; align-items: center; justify-content: space-between; }
  h2 { margin: 0; font-size: 1rem; color: white; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .close-btn { background: none; border: none; color: #ccc; font-size: 1.25rem; cursor: pointer; padding: .25rem; }
  .close-btn:hover { color: white; }
  .profile-select { color: #ccc; font-size: .875rem; }
  .profile-select select { margin-left: .5rem; background: #333; color: white; border: 1px solid #555; border-radius: 4px; padding: .25rem .5rem; }
  .player-wrap { width: 100%; aspect-ratio: 16/9; background: black; border-radius: 4px; overflow: hidden; }
  .video-player { width: 100%; height: 100%; }
  .error { color: #f87171; font-size: .875rem; margin: 0; }
</style>
