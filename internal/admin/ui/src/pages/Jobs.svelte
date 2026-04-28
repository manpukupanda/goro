<script>
  import { api } from '../lib/api.js';

  let jobs = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await api.listJobs();
      jobs = res.jobs ?? [];
    } catch (err) {
      error = err.message;
    } finally {
      loading = false;
    }
  }

  $effect(() => { load(); });

  const statusLabel = { pending: '待機中', processing: '処理中', done: '完了', failed: '失敗' };
  const statusClass = { pending: 'badge-queued', processing: 'badge-processing', done: 'badge-ready', failed: 'badge-failed' };
</script>

<div class="page">
  <div class="page-header">
    <h2>ジョブ一覧</h2>
    <button class="btn-secondary" onclick={load}>更新</button>
  </div>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if loading}
    <p class="muted">読み込み中...</p>
  {:else if jobs.length === 0}
    <p class="muted">ジョブがありません</p>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>ジョブ ID</th>
            <th>動画 ID</th>
            <th>ステータス</th>
            <th>作成日時</th>
            <th>更新日時</th>
            <th>エラー</th>
          </tr>
        </thead>
        <tbody>
          {#each jobs as j (j.id)}
            <tr>
              <td class="mono">{j.id}</td>
              <td class="mono">{j.video_public_id ?? j.video_id}</td>
              <td><span class="badge {statusClass[j.status] ?? ''}">{statusLabel[j.status] ?? j.status}</span></td>
              <td class="mono">{j.created_at ?? ''}</td>
              <td class="mono">{j.updated_at ?? ''}</td>
              <td class="error-cell">{j.error_message ?? ''}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; }
  .page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
  h2 { margin: 0; }
  .btn-secondary { padding: .4rem .8rem; background: #f3f4f6; color: #374151; border: 1px solid #d1d5db; border-radius: 4px; cursor: pointer; font-size: .875rem; }
  .btn-secondary:hover { background: #e5e7eb; }
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
  .error-cell { color: #dc2626; font-size: .8rem; max-width: 300px; word-break: break-word; }
  .error { color: #dc2626; font-size: .875rem; }
  .muted { color: #9ca3af; }
</style>
