<script>
  import { onMount } from 'svelte'
  import { call, errorText } from './api.js'

  let { ov, run } = $props()
  let db = $state(null)
  let ssl = $state(null)
  let reveal = $state(false)
  let sslBusy = $state('')
  let sslError = $state('')
  let copied = $state('')

  async function load() {
    db = await call('Database')
    ssl = await call('SSL')
  }
  onMount(load)

  async function https(method, label) {
    sslBusy = label
    sslError = ''
    try {
      ssl = await call(method)
    } catch (e) {
      sslError = errorText(e)
      ssl = await call('SSL')
    } finally {
      sslBusy = ''
    }
  }

  async function copy(text, what) {
    await navigator.clipboard.writeText(text)
    copied = what
    setTimeout(() => (copied = ''), 1500)
  }

  let envLine = $derived(db ? `DB_HOST=127.0.0.1\nDB_PORT=${db.port}\nDB_USERNAME=${db.user}\nDB_PASSWORD=${db.password}` : '')
</script>

<h2>Settings</h2>

<div class="grid">
  <section class="card">
    <h3>Database</h3>
    {#if !db}
      <p class="muted">Loading…</p>
    {:else if !db.installed}
      <p class="muted">No database installed. In a terminal, run <code>ebur install mariadb</code>.</p>
    {:else}
      <dl>
        <dt>Server</dt><dd>MariaDB {db.version}</dd>
        <dt>Host</dt><dd class="mono">127.0.0.1:{db.port}</dd>
        <dt>User</dt><dd class="mono">{db.user}</dd>
        <dt>Password</dt>
        <dd class="mono">
          {reveal ? db.password : '••••••••••••'}
          <button class="link" onclick={() => (reveal = !reveal)}>{reveal ? 'Hide' : 'Show'}</button>
        </dd>
      </dl>
      <div class="row">
        <button onclick={() => copy(envLine, 'env')}>{copied === 'env' ? 'Copied' : 'Copy .env settings'}</button>
        {#if db.adminer}<button onclick={() => run('', 'OpenURL', db.adminer)}>Open Adminer</button>{/if}
      </div>
    {/if}
  </section>

  <section class="card">
    <h3>HTTPS</h3>
    {#if !ssl}
      <p class="muted">Loading…</p>
    {:else if !ssl.exists}
      <p>Serve sites at <code>https://&lt;site&gt;.{ov.tld}</code> without browser warnings. Windows will ask you to confirm trusting Ebur's local certificate authority; it can only sign <code>.{ov.tld}</code> and <code>localhost</code> names.</p>
      <button class="primary" disabled={!!sslBusy} onclick={() => https('EnableHTTPS', 'Waiting for the Windows dialog…')}>{sslBusy || 'Enable HTTPS'}</button>
    {:else}
      <dl>
        <dt>Status</dt><dd><span class="dot {ssl.trusted ? 'ok' : 'warn'}"></span>{ssl.trusted ? 'Trusted by Windows' : 'Not trusted: browsers will warn'}</dd>
        <dt>Authority</dt><dd>{ssl.name}</dd>
        <dt>SHA-256</dt><dd class="mono fp">{ssl.fingerprint}</dd>
        <dt>Valid to</dt><dd>{ssl.validTo}</dd>
      </dl>
      <div class="row">
        {#if !ssl.trusted}<button class="primary" disabled={!!sslBusy} onclick={() => https('EnableHTTPS', 'Waiting…')}>Trust it</button>{/if}
        <button disabled={!!sslBusy} onclick={() => https('DisableHTTPS', 'Waiting for the Windows dialog…')}>{sslBusy || 'Remove HTTPS'}</button>
      </div>
    {/if}
    {#if sslError}<pre class="err">{sslError}</pre>{/if}
  </section>

  <section class="card">
    <h3>Folders</h3>
    <dl>
      <dt>Ebur</dt><dd class="mono">{ov.root}</dd>
      <dt>Sites</dt><dd class="mono">{ov.root}\www</dd>
    </dl>
    <div class="row">
      <button onclick={() => run('', 'OpenWWW')}>Open www</button>
      <button onclick={() => run('', 'OpenLogs')}>Open logs</button>
      <button onclick={() => run('', 'OpenTerminal')}>Open terminal</button>
    </div>
  </section>

  <section class="card">
    <h3>About</h3>
    <p class="muted">Ebur {ov.version}. Open source, MIT licence. The <code>ebur</code> command line has the same features as this window.</p>
  </section>
</div>

<style>
  h2 { margin: 0 0 14px; font-size: 18px; }
  h3 { margin: 0 0 10px; font-size: 15px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(330px, 1fr)); gap: 14px; }
  dl { display: grid; grid-template-columns: 90px 1fr; gap: 6px 10px; margin: 0 0 12px; }
  dt { color: var(--muted); }
  dd { margin: 0; overflow-wrap: anywhere; }
  .fp { font-size: 11px; }
  p { margin: 0 0 12px; }
  .err { white-space: pre-wrap; color: var(--red); font-family: var(--font-ui); margin: 10px 0 0; }
</style>
