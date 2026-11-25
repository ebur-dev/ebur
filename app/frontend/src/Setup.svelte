<script>
  // First-run wizard (SRS §4.1): welcome, choices, install with progress, done.
  import { onMount, onDestroy } from 'svelte'
  import { call, on, errorText } from './api.js'
  import icon from '../../icon/ebur-256.png'

  let { onfinish } = $props()

  let page = $state('welcome') // welcome | choose | install | done
  let opts = $state(null)
  let error = $state('')
  let php = $state('')
  let db = $state('')
  let adminer = $state(true)
  let mail = $state(true)
  let node = $state(false)
  let redis = $state(false)
  let https = $state(true)
  let steps = $state([])
  let result = $state(null)
  let offs = []

  onMount(async () => {
    try {
      opts = await call('SetupOptions')
      php = opts.phpDefault
      db = opts.dbDefault
    } catch (e) {
      error = errorText(e)
    }
    offs.push(await on('setup:progress', (s) => (steps = s ?? [])))
    offs.push(
      await on('setup:finished', (r) => {
        result = r
        page = 'done'
      }),
    )
  })
  onDestroy(() => offs.forEach((off) => off?.()))

  async function begin() {
    error = ''
    page = 'install'
    steps = []
    try {
      await call('RunSetup', { php, db, dbPort: opts.dbPort, adminer: db !== '' && adminer, mail, node, redis, https })
    } catch (e) {
      error = errorText(e)
      page = 'choose'
    }
  }

  const pct = (s) => (s.total > 0 ? Math.min(100, Math.round((s.done * 100) / s.total)) : null)
  const mb = (n) => (n / 1048576).toFixed(1)
</script>

<div class="wizard">
  <div class="panel">
    <div class="brand"><img src={icon} alt="" width="44" height="44" /><span>ebur</span></div>

    {#if page === 'welcome'}
      <h1>Welcome to Ebur</h1>
      <p>Ebur runs PHP sites on this computer: every folder in <code>www\</code> becomes <code>https://&lt;folder&gt;.test</code>.</p>
      <p class="muted">Setup downloads PHP, Nginx, Composer and, if you want one, a MariaDB database, from their official sites. Every download is checked against a published checksum. Nothing is changed outside <code>{opts?.root ?? 'the Ebur folder'}</code> except two things you approve: trusting Ebur's local certificate, and adding your site names to the hosts file.</p>
      {#if error}<pre class="err">{error}</pre>{/if}
      <div class="actions"><button class="primary" disabled={!opts} onclick={() => (page = 'choose')}>Get started</button></div>

    {:else if page === 'choose'}
      <h1>Choose your setup</h1>

      <label class="field">
        <span>PHP version</span>
        <select bind:value={php}>
          {#each opts.php as v}<option value={v}>{v}{v === opts.phpDefault ? ' (recommended)' : ''}</option>{/each}
        </select>
        <small class="muted">You can install more versions later and switch from the tray.</small>
      </label>

      <div class="field">
        <span>Web server</span>
        <div>{opts.webServer}</div>
      </div>

      <fieldset class="field">
        <legend>Database</legend>
        {#each opts.db as o}
          <label class="radio"><input type="radio" bind:group={db} value={o.version} /> {o.label}</label>
        {/each}
        <label class="radio"><input type="radio" bind:group={db} value="" /> No database</label>
        {#if db}
          <label class="radio"><input type="checkbox" bind:checked={adminer} /> Adminer, to browse databases at <code>db.{opts.tld}</code></label>
          <small class="muted">Port {opts.dbPort}. {opts.dbPortNote}</small>
        {/if}
      </fieldset>

      <fieldset class="field">
        <legend>Mail</legend>
        <label class="radio"><input type="checkbox" bind:checked={mail} /> Catch outgoing email in a local inbox at <code>mail.{opts.tld}</code></label>
        <small class="muted">Password resets and sign-up emails from your sites land there instead of reaching real people (Mailpit).</small>
      </fieldset>

      <fieldset class="field">
        <legend>Node.js</legend>
        <label class="radio"><input type="checkbox" bind:checked={node} /> Install Node.js for front-end tools (npm, Vite)</label>
        <small class="muted">Laravel and most modern themes build CSS and JavaScript with it. About 30 MB; you can add it later in Settings. A Node you already have keeps working.</small>
      </fieldset>

      <fieldset class="field">
        <legend>Redis</legend>
        <label class="radio"><input type="checkbox" bind:checked={redis} /> Install Redis for caching, sessions and queues</label>
        <small class="muted">Runs on this computer only (127.0.0.1:6379), with PHP's redis extension. About 14 MB; you can add it later in Settings.</small>
      </fieldset>

      <fieldset class="field">
        <legend>HTTPS</legend>
        <label class="radio"><input type="checkbox" bind:checked={https} /> Serve sites over <code>https://</code> without browser warnings</label>
        <small class="muted">Windows will ask you to confirm trusting Ebur's local certificate. It can only vouch for <code>.{opts.tld}</code> and <code>localhost</code> names.</small>
      </fieldset>

      {#each opts.warnings as w}<p class="warn">{w}</p>{/each}
      {#if error}<pre class="err">{error}</pre>{/if}
      <div class="actions">
        <button onclick={() => (page = 'welcome')}>Back</button>
        <button class="primary" onclick={begin}>Install</button>
      </div>

    {:else if page === 'install'}
      <h1>Setting up Ebur</h1>
      <p class="muted">This takes a minute or two. Windows may ask for permission near the end.</p>
      <ul class="steps">
        {#each steps as s}
          <li class={s.state}>
            <span class="icon">{s.state === 'done' ? '✓' : s.state === 'failed' ? '!' : s.state === 'running' ? '•' : ''}</span>
            <div class="body">
              <div class="label">{s.label}</div>
              {#if s.state === 'running' && s.detail === 'Downloading' && pct(s) !== null}
                <div class="bar"><div style="width: {pct(s)}%"></div></div>
                <div class="detail">{mb(s.done)} of {mb(s.total)} MB</div>
              {:else if s.detail}
                <div class="detail">{s.detail}</div>
              {/if}
            </div>
          </li>
        {/each}
      </ul>

    {:else}
      {#if result?.ok}
        <h1>Ebur is ready</h1>
        <p>Create a folder in <code>{opts.root}\www</code>, for example <code>shop</code>, and open <code>{https ? 'https' : 'http'}://shop.{opts.tld}</code>.</p>
        {#if result.notes.length}
          <div class="notes">
            <strong>Finished with notes:</strong>
            <ul>{#each result.notes as n}<li>{n}</li>{/each}</ul>
            <small class="muted">You can fix these later in Settings.</small>
          </div>
        {/if}
        <div class="actions">
          <button onclick={() => call('OpenWWW')}>Open www folder</button>
          <button class="primary" onclick={onfinish}>Open Ebur</button>
        </div>
      {:else}
        <h1>Setup stopped</h1>
        <pre class="err">{result?.error}</pre>
        <p class="muted">Finished steps are kept, so trying again continues where it stopped.</p>
        <div class="actions"><button class="primary" onclick={() => (page = 'choose')}>Try again</button></div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .wizard { height: 100%; overflow: auto; display: flex; justify-content: center; padding: 32px 16px; }
  .panel { width: 100%; max-width: 620px; }
  .brand { display: flex; align-items: center; gap: 12px; font-size: 24px; font-weight: 700; margin-bottom: 20px; }
  .brand img { border-radius: 10px; }
  h1 { font-size: 22px; margin: 0 0 12px; }
  p { margin: 0 0 12px; }
  .field { display: flex; flex-direction: column; gap: 6px; margin: 0 0 18px; border: none; padding: 0; }
  .field > span, legend { font-weight: 600; padding: 0; margin-bottom: 4px; }
  .radio { display: flex; gap: 8px; align-items: center; }
  .actions { display: flex; gap: 8px; justify-content: flex-end; margin-top: 22px; }
  .warn { color: var(--gold); }
  .err { white-space: pre-wrap; color: var(--red); font-family: var(--font-ui); }
  .steps { list-style: none; padding: 0; margin: 16px 0 0; }
  .steps li { display: flex; gap: 12px; padding: 10px 0; border-bottom: 1px solid var(--border); }
  .icon { width: 22px; height: 22px; flex: none; border-radius: 50%; display: grid; place-items: center; font-size: 13px; font-weight: 700; border: 1px solid var(--border); color: var(--muted); }
  .done .icon { background: var(--green); border-color: var(--green); color: #fff; }
  .running .icon { border-color: var(--accent); color: var(--accent); }
  .failed .icon { background: var(--red); border-color: var(--red); color: #fff; }
  .pending .label { color: var(--muted); }
  .body { flex: 1; min-width: 0; }
  .detail { font-size: 13px; color: var(--muted); overflow-wrap: anywhere; }
  .failed .detail { color: var(--red); }
  .bar { height: 6px; background: var(--surface-2); border-radius: 3px; overflow: hidden; margin: 6px 0 4px; }
  .bar div { height: 100%; background: var(--accent); transition: width 0.2s; }
  .notes { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 12px 14px; }
  .notes ul { margin: 6px 0; padding-left: 18px; }
</style>
