<script>
  import { isAuthenticated } from './lib/auth.js';
  import LoginForm from './components/LoginForm.svelte';
  import Videos from './pages/Videos.svelte';
  import Jobs from './pages/Jobs.svelte';

  let loggedIn = $state(isAuthenticated());
  let currentPage = $state('videos');

  function handleLogin() {
    loggedIn = true;
  }
</script>

{#if !loggedIn}
  <LoginForm onLogin={handleLogin} />
{:else}
  <div class="layout">
    <nav class="sidebar">
      <div class="logo">goro Admin</div>
      <ul>
        <li>
          <button
            class:active={currentPage === 'videos'}
            onclick={() => { currentPage = 'videos'; }}
          >🎬 Videos</button>
        </li>
        <li>
          <button
            class:active={currentPage === 'jobs'}
            onclick={() => { currentPage = 'jobs'; }}
          >⚙️ Jobs</button>
        </li>
      </ul>
    </nav>
    <main class="content">
      {#if currentPage === 'videos'}
        <Videos />
      {:else if currentPage === 'jobs'}
        <Jobs />
      {/if}
    </main>
  </div>
{/if}

<style>
  :global(*, *::before, *::after) { box-sizing: border-box; }
  :global(body) { margin: 0; font-family: system-ui, sans-serif; background: #f9fafb; color: #111827; }
  .layout { display: flex; height: 100vh; overflow: hidden; }
  .sidebar {
    width: 200px; background: #1e293b; color: white;
    display: flex; flex-direction: column; flex-shrink: 0;
  }
  .logo {
    padding: 1.25rem 1rem; font-weight: 700; font-size: 1rem;
    border-bottom: 1px solid rgba(255,255,255,.1);
  }
  ul { list-style: none; margin: 0; padding: .5rem 0; }
  li button {
    width: 100%; text-align: left; padding: .6rem 1rem;
    background: none; border: none; color: #94a3b8;
    cursor: pointer; font-size: .9rem; border-radius: 0;
    transition: background .15s, color .15s;
  }
  li button:hover { background: rgba(255,255,255,.08); color: white; }
  li button.active { background: rgba(255,255,255,.12); color: white; font-weight: 600; }
  .content { flex: 1; overflow-y: auto; }
</style>
