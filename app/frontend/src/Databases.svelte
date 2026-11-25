<script>
  // Databases and their snapshots (TOOL-05). A restore saves the current
  // data first, so it can always be undone from the list.
  import { onMount } from 'svelte'
  import { call, errorText } from './api.js'

  let { ov, run } = $props()

  let list = $state(null)
  let open = $state({}) // database name -> snapshots shown
  let labeling = $state('') // database whose label box is open
  let label = $state('')
  let confirm = $state(null) // { kind: 'restore' | 'delete', db, snap }
  let busy = $state('')
  let error = $state('')
  let notice = $state('')

  async function load() {
    list = await call('Databases')
  }
  onMount(load)

  async function act(text, fn) {
    busy = text
    error = ''
    notice = ''
    try {
      notice = (await fn()) || ''
    } catch (e) {
      error = errorText(e)
    } finally {
      busy = ''
      confirm = null
      await load()
    }
  }

  const save = (db) =>
    act(`Saving a snapshot of ${db}…`, async () => {
      const s = await call('SnapshotDB', db, label)
      labeling = ''
      label = ''
      open[db] = true
      return `Saved ${s.name} (${size(s.bytes)}).`
    })

  const restore = (db, snap) =>
    act(`Restoring ${db}…`, async () => {
      const r = await call('RestoreDB', db, snap.name)
      return `Restored ${db} from ${snap.name}.` + (r.backup ? ` The data it replaced was saved as ${r.backup.name}; restore that to undo.` : '')
    })

  const remove = (db, snap) =>
    act('Deleting…', async () => {
      await call('DeleteSnapshot', db, snap.name)
      return `Deleted snapshot ${snap.name}.`
    })

  function size(n) {
    if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + ' GB'
    if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB'
    if (n >= 1 << 10) return Math.round(n / 1024) + ' KB'
    return n + ' B'
  }
  const when = (t) => new Date(t).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
</script>

<div class="row head">
  <h2>Databases</h2>
  <span class="muted">Snapshots save a database so you can put it back later, e.g. before a migration.</span>
  <span class="spacer"></span>
  <button onclick={load} disabled={!!busy}>Refresh</button>
  <button onclick={() => run('', 'OpenSnapshots')}>Open snapshots folder</button>
</div>

{#if busy}<p class="status"><span class="spinner"></span>{busy}</p>{/if}
{#if notice}<p class="notice">{notice}</p>{/if}
{#if error}<pre class="err">{error}</pre>{/if}

{#if !list}
  <p class="muted">Loading…</p>
{:else if !list.installed}
  <div class="card"><p>No database is installed. In a terminal, run <code>ebur install mariadb</code>.</p></div>
{:else if !list.running}
  <div class="card">
    <p>MariaDB is not running, so its databases cannot be listed.</p>
    <button class="primary" onclick={async () => { await run('Starting…', 'Start'); load() }}>Start services</button>
  </div>
{:else if list.databases.length === 0}
  <div class="card"><p class="muted">No databases yet. <strong>New project…</strong> on the Sites tab creates one with each Laravel, WordPress or Symfony site.</p></div>
{:else}
  <div class="card flush">
    <table>
      <thead><tr><th>Database</th><th>Site</th><th>Size</th><th>Snapshots</th><th></th></tr></thead>
      <tbody>
        {#each list.databases as d}
          <tr>
            <td><strong class="mono">{d.name}</strong><div class="muted small">{d.tables} {d.tables === 1 ? 'table' : 'tables'}</div></td>
            <td>{d.sites.join(', ') || '—'}</td>
            <td>{size(d.bytes)}</td>
            <td>
              {#if d.snapshots.length}
                <button class="link" onclick={() => (open[d.name] = !open[d.name])}>{d.snapshots.length} saved {open[d.name] ? '▴' : '▾'}</button>
              {:else}<span class="muted">none</span>{/if}
            </td>
            <td class="actions">
              {#if labeling === d.name}
                <input bind:value={label} placeholder="label (optional)" maxlength="40"
                  onkeydown={(e) => e.key === 'Enter' && save(d.name)} />
                <button class="primary" disabled={!!busy} onclick={() => save(d.name)}>Save</button>
                <button disabled={!!busy} onclick={() => { labeling = ''; label = '' }}>Cancel</button>
              {:else}
                <button disabled={!!busy} onclick={() => { labeling = d.name; label = '' }}>Snapshot…</button>
              {/if}
            </td>
          </tr>
          {#if open[d.name] && d.snapshots.length}
            <tr class="snaps">
              <td colspan="5">
                <ul>
                  {#each d.snapshots as s}
                    <li>
                      <span>{when(s.time)}</span>
                      {#if s.label}<span class="tag" class:auto={s.auto}>{s.auto ? 'before a restore' : s.label}</span>{/if}
                      <span class="muted small">{size(s.bytes)}</span>
                      <span class="spacer"></span>
                      {#if confirm && confirm.snap.name === s.name && confirm.db === d.name}
                        <span class="ask">
                          {#if confirm.kind === 'restore'}
                            Replace <strong>{d.name}</strong> with this snapshot? Its current data is saved first.
                            <button class="primary" disabled={!!busy} onclick={() => restore(d.name, s)}>Restore</button>
                          {:else}
                            Delete this snapshot file?
                            <button class="danger" disabled={!!busy} onclick={() => remove(d.name, s)}>Delete</button>
                          {/if}
                          <button disabled={!!busy} onclick={() => (confirm = null)}>Cancel</button>
                        </span>
                      {:else}
                        <button disabled={!!busy} onclick={() => (confirm = { kind: 'restore', db: d.name, snap: s })}>Restore…</button>
                        <button class="link small" disabled={!!busy} onclick={() => (confirm = { kind: 'delete', db: d.name, snap: s })}>Delete</button>
                      {/if}
                    </li>
                  {/each}
                </ul>
              </td>
            </tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
  <p class="muted note">Snapshots are compressed SQL dumps in <code>{list.folder}</code>. The command line has the same: <code>ebur db snapshot shop</code>, <code>ebur db restore shop latest</code>.</p>
{/if}

<style>
  h2 { margin: 0; font-size: 18px; }
  .head { margin-bottom: 14px; flex-wrap: wrap; }
  .spacer { flex: 1; }
  .flush { padding: 0; overflow: hidden; }
  .small { font-size: 12px; }
  .note { font-size: 13px; margin-top: 10px; overflow-wrap: anywhere; }
  .actions { white-space: nowrap; text-align: right; }
  .actions input {
    font: inherit; font-size: 13px; padding: 4px 8px; width: 150px; border-radius: 6px;
    border: 1px solid var(--border); background: var(--surface); color: var(--text);
  }
  .snaps td { background: var(--surface-2); padding-top: 6px; padding-bottom: 6px; }
  .snaps ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .snaps li { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .tag { font-size: 12px; padding: 1px 8px; border-radius: 10px; background: color-mix(in srgb, var(--accent) 22%, transparent); }
  .tag.auto { background: color-mix(in srgb, var(--grey) 25%, transparent); }
  .ask { display: inline-flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 13px; }
  .danger { border-color: var(--red); color: var(--red); }
  .status { display: flex; align-items: center; gap: 10px; }
  .notice { color: var(--green); }
  .err { white-space: pre-wrap; color: var(--red); font-family: var(--font-ui); font-size: 13px; }
  .spinner {
    width: 14px; height: 14px; border-radius: 50%; flex: none;
    border: 2px solid var(--border); border-top-color: var(--accent); animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
