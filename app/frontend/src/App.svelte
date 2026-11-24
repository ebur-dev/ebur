<script>
  import { onMount } from 'svelte'
  import { call, errorText } from './api.js'
  import Sites from './Sites.svelte'
  import Services from './Services.svelte'
  import Logs from './Logs.svelte'
  import Health from './Health.svelte'
  import Settings from './Settings.svelte'

  const tabs = ['Sites', 'Services', 'Logs', 'Health', 'Settings']
  const summaryText = { all: 'All services running', some: 'Some services running', none: 'Services stopped' }

  let tab = $state('Sites')
  let ov = $state(null)
  let busy = $state('')
  let error = $state('')

  async function refresh() {
    try {
      ov = await call('Overview')
    } catch (e) {
      error = errorText(e)
    }
  }

  // run performs an action, shows it as busy, and surfaces errors.
  async function run(label, method, ...args) {
    busy = label
    error = ''
    try {
      await call(method, ...args)
    } catch (e) {
      error = errorText(e)
    } finally {
      busy = ''
      await refresh()
    }
  }

  onMount(() => {
    refresh()
    const t = setInterval(refresh, 2000)
    return () => clearInterval(t)
  })
</script>

<div class="shell">
  <header>
    <div class="brand">
      <span class="logo">ebur</span>
      {#if ov}
        <span class="status"><span class="dot {ov.status.summary}"></span>{busy || summaryText[ov.status.summary]}</span>
      {/if}
    </div>
    {#if ov}
      <div class="row">
        <label class="php">
          PHP
          <select
            value={ov.phpActive}
            disabled={!!busy || ov.phpInstalled.length === 0}
            onchange={(e) => run('Switching PHP…', 'SetPHP', e.currentTarget.value)}
          >
            {#each ov.phpInstalled as v}<option value={v}>{v}</option>{/each}
          </select>
        </label>
        {#if ov.status.summary !== 'all'}
          <button class="primary" disabled={!!busy} onclick={() => run('Starting…', 'Start')}>Start all</button>
        {/if}
        {#if ov.status.summary !== 'none'}
          <button disabled={!!busy} onclick={() => run('Restarting…', 'Restart')}>Restart</button>
          <button disabled={!!busy} onclick={() => run('Stopping…', 'Stop')}>Stop all</button>
        {/if}
      </div>
    {/if}
  </header>

  <nav>
    {#each tabs as t}
      <button class:active={tab === t} onclick={() => (tab = t)}>{t}</button>
    {/each}
  </nav>

  {#if error}
    <div class="error" role="alert">
      <pre>{error}</pre>
      <button class="link" onclick={() => (error = '')}>Dismiss</button>
    </div>
  {/if}

  <main>
    {#if !ov}
      <p class="muted">Loading…</p>
    {:else if tab === 'Sites'}
      <Sites {ov} {run} />
    {:else if tab === 'Services'}
      <Services {ov} {run} {busy} />
    {:else if tab === 'Logs'}
      <Logs />
    {:else if tab === 'Health'}
      <Health />
    {:else}
      <Settings {ov} {run} />
    {/if}
  </main>
</div>

<style>
  .shell { display: flex; flex-direction: column; height: 100%; }
  header {
    display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap;
    padding: 14px 20px; background: var(--ink); color: var(--ivory);
  }
  .brand { display: flex; align-items: baseline; gap: 16px; }
  .logo { font-size: 22px; font-weight: 700; letter-spacing: 0.02em; }
  .status { font-size: 13px; opacity: 0.9; display: inline-flex; align-items: center; }
  header .php { display: inline-flex; gap: 6px; align-items: center; font-size: 13px; }
  header button:not(.primary) { background: transparent; color: var(--ivory); border-color: #5b6168; }
  header select { background: #3a4045; color: var(--ivory); border-color: #5b6168; }
  nav { display: flex; gap: 4px; padding: 8px 16px 0; border-bottom: 1px solid var(--border); background: var(--surface-2); }
  nav button { border: none; border-bottom: 2px solid transparent; border-radius: 0; background: none; padding: 8px 12px; color: var(--muted); }
  nav button.active { color: var(--text); border-bottom-color: var(--accent); font-weight: 600; }
  main { flex: 1; overflow: auto; padding: 18px 20px; }
  .error {
    margin: 12px 20px 0; padding: 10px 14px; border-radius: 8px; display: flex; justify-content: space-between; gap: 12px;
    background: color-mix(in srgb, var(--red) 12%, var(--surface)); border: 1px solid color-mix(in srgb, var(--red) 40%, var(--border));
  }
  .error pre { margin: 0; white-space: pre-wrap; font-family: var(--font-ui); font-size: 13px; }
</style>
