<script>
  let { ov, run, busy } = $props()

  // Group php-cgi workers ("php-8.5.13#1".."#4") into one row per pool.
  let groups = $derived.by(() => {
    const m = new Map()
    for (const s of ov.status.services) {
      const name = s.Name.split('#')[0]
      const g = m.get(name) ?? { name, total: 0, running: 0, pids: [], error: '', lines: [] }
      g.total++
      if (s.State === 'running') {
        g.running++
        g.pids.push(s.PID)
      }
      if (s.Error) {
        g.error = s.Error
        g.lines = s.LastLines ?? []
      }
      m.set(name, g)
    }
    return [...m.values()].sort((a, b) => a.name.localeCompare(b.name))
  })

  function state(g) {
    if (g.running === g.total) return g.total > 1 ? `Running (${g.total} workers)` : 'Running'
    if (g.running > 0) return `Partly running (${g.running}/${g.total})`
    return g.error ? 'Failed' : 'Stopped'
  }

  function cls(g) {
    if (g.running === g.total) return 'running'
    if (g.running > 0) return 'some'
    return g.error ? 'failed' : ''
  }

  // "php-8.5.13" -> "php" for start/stop by group
  const key = (name) => (name.startsWith('php-') ? 'php' : name)
</script>

<h2>Services</h2>
{#if groups.length === 0}
  <div class="card">
    <p class="muted">Nothing has been started yet. <button class="link" onclick={() => run('Starting…', 'Start')}>Start all services</button></p>
  </div>
{:else}
  <div class="card flush">
    <table>
      <thead><tr><th>Service</th><th>Status</th><th>Process IDs</th><th></th></tr></thead>
      <tbody>
        {#each groups as g}
          <tr>
            <td><strong>{g.name}</strong></td>
            <td><span class="dot {cls(g)}"></span>{state(g)}</td>
            <td class="mono muted">{g.pids.join(', ') || '–'}</td>
            <td class="actions">
              {#if g.running < g.total}
                <button disabled={!!busy} onclick={() => run('Starting ' + g.name + '…', 'StartService', key(g.name))}>Start</button>
              {/if}
              {#if g.running > 0}
                <button disabled={!!busy} onclick={() => run('Stopping ' + g.name + '…', 'StopService', key(g.name))}>Stop</button>
              {/if}
            </td>
          </tr>
          {#if g.error}
            <tr class="fail">
              <td colspan="4">
                <div>{g.error}</div>
                {#if g.lines.length}<pre>{g.lines.slice(-8).join('\n')}</pre>{/if}
              </td>
            </tr>
          {/if}
        {/each}
      </tbody>
    </table>
  </div>
  <p class="muted">Listening on {ov.status.info.listen || '127.0.0.1:80'}{ov.status.info.https ? ' and :443 (HTTPS)' : ''}. Services only accept connections from this computer.</p>
{/if}

<style>
  h2 { margin: 0 0 14px; font-size: 18px; }
  .flush { padding: 0; overflow: hidden; }
  .actions { text-align: right; white-space: nowrap; }
  .actions button { margin-left: 6px; }
  .fail td { background: color-mix(in srgb, var(--red) 8%, var(--surface)); }
  .fail pre { margin: 8px 0 0; white-space: pre-wrap; font-family: var(--font-mono); font-size: 12px; }
</style>
