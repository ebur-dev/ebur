<script>
  import { onMount, tick } from 'svelte'
  import { call, errorText } from './api.js'

  let files = $state([])
  let selected = $state('')
  let lines = $state([])
  let follow = $state(true)
  let error = $state('')
  let pre = $state()

  async function load() {
    try {
      files = await call('Logs')
      if (!selected && files.length) selected = files[0].name
      if (selected) {
        lines = (await call('ReadLog', selected, 400)) ?? []
        if (follow) {
          await tick()
          pre?.scrollTo(0, pre.scrollHeight)
        }
      }
      error = ''
    } catch (e) {
      error = errorText(e)
    }
  }

  onMount(() => {
    load()
    const t = setInterval(() => follow && load(), 2000)
    return () => clearInterval(t)
  })
</script>

<div class="row head">
  <h2>Logs</h2>
  <select bind:value={selected} onchange={load}>
    {#each files as f}<option value={f.name}>{f.name}</option>{/each}
  </select>
  <label class="row"><input type="checkbox" bind:checked={follow} /> Follow</label>
  <span class="spacer"></span>
  <button onclick={() => call('OpenLogs')}>Open logs folder</button>
</div>

{#if error}<p class="muted">{error}</p>{/if}
{#if files.length === 0}
  <p class="muted">No logs yet. They appear once services have started.</p>
{:else}
  <pre bind:this={pre}>{lines.join('\n') || '(empty)'}</pre>
{/if}

<style>
  h2 { margin: 0; font-size: 18px; }
  .head { margin-bottom: 12px; }
  .spacer { flex: 1; }
  pre {
    margin: 0; height: calc(100vh - 230px); min-height: 200px; overflow: auto; padding: 12px;
    background: var(--ink); color: #e9e4d6; border-radius: 8px; white-space: pre-wrap; word-break: break-all;
    font-variant-ligatures: none;
  }
</style>
