<script>
  import { setCredentials } from '../lib/auth.js';

  let username = $state('');
  let password = $state('');
  let error = $state('');

  const { onLogin } = $props();

  async function handleSubmit(e) {
    e.preventDefault();
    if (!username || !password) {
      error = 'Please enter your username and password.';
      return;
    }
    setCredentials(username, password);
    onLogin();
  }
</script>

<div class="login-wrap">
  <form class="login-box" onsubmit={handleSubmit}>
    <h1>goro Admin Console</h1>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <label>
      Username
      <input type="text" bind:value={username} autocomplete="username" required />
    </label>
    <label>
      Password
      <input type="password" bind:value={password} autocomplete="current-password" required />
    </label>
    <button type="submit">Login</button>
  </form>
</div>

<style>
  .login-wrap {
    display: flex;
    justify-content: center;
    align-items: center;
    height: 100vh;
    background: #f0f2f5;
  }
  .login-box {
    background: white;
    border-radius: 8px;
    padding: 2rem;
    width: 320px;
    box-shadow: 0 2px 12px rgba(0,0,0,.15);
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  h1 { margin: 0 0 .5rem; font-size: 1.25rem; text-align: center; }
  label { display: flex; flex-direction: column; gap: .25rem; font-size: .875rem; font-weight: 600; }
  input { padding: .5rem .75rem; border: 1px solid #ccc; border-radius: 4px; font-size: 1rem; }
  button { padding: .6rem; background: #2563eb; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 1rem; }
  button:hover { background: #1d4ed8; }
  .error { color: #dc2626; font-size: .875rem; margin: 0; }
</style>
