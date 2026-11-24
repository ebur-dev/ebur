<script>
  import { onMount } from 'svelte'
  import { call } from './api.js'

  let checks = $state([])
  let running = $state(false)

  async function check() {
    running = true
    try {
      checks = await call('Doctor')
    } finally {
      running = false
    }
  }

  onMount(check)

  let counts = $derived({
    fail: checks.filter((c) => c.level === 'fail').length,
    warn: checks.filter((c) => c.level === 'warn').length,
  })
</script>

<div class="row head">
  <h2>Health</h2>
  <span class="muted">{counts.fail} problem{counts.fail === 1 ? '' : 's'}, {counts.warn} warning{counts.warn === 1 ? '' : 's'}</span>
  <span class="spacer"></span>
  <button onclick={check} disabled={running}>{running ? 'Checking…' : 'Check again'}</button>
</div>

<div class="card flush">
  {#each checks as c}
    <div class="check {c.level}">
      <span class="dot {c.level}"></span>
      <div>
        <div><strong>{c.name}</strong> <span class="muted">{c.detail}</span></div>
        {#if c.hint && c.level !== 'ok'}<div class="hint">{c.hint}</div>{/if}
      </div>
    </div>
  {/each}
</div>

<style>
  h2 { margin: 0; font-size: 18px; }
  .head { margin-bottom: 14px; }
  .spacer { flex: 1; }
  .flush { padding: 0; }
  .check { display: flex; gap: 6px; padding: 10px 14px; border-bottom: 1px solid var(--border); }
  .check:last-child { border-bottom: none; }
  .check .dot { margin-top: 6px; flex: none; }
  .check.info .dot { background: var(--grey); }
  .hint { margin-top: 3px; font-size: 13px; }
  .check.fail .hint, .check.warn .hint { color: var(--text); }
</style>
