<script>
  import { onMount } from 'svelte'
  import { call, errorText } from './api.js'
  import { applyTheme } from './theme.js'

  let { ov, run } = $props()
  let appearance = $state('system')
  const modes = [
    { id: 'system', label: 'System' },
    { id: 'light', label: 'Light' },
    { id: 'dark', label: 'Dark' },
  ]

  async function setAppearance(mode) {
    appearance = mode
    applyTheme(mode)
    try {
      await call('SetAppearance', mode)
    } catch (e) {
      sslError = errorText(e)
    }
  }
  let db = $state(null)
  let ssl = $state(null)
  let reveal = $state(false)
  let sslBusy = $state('')
  let sslError = $state('')
  let copied = $state('')

  let hosts = $state(null)
  let hostsBusy = $state(false)
  let hostsError = $state('')

  async function syncHosts() {
    hostsBusy = true
    hostsError = ''
    try {
      hosts = await call('SyncHosts')
    } catch (e) {
      hostsError = errorText(e)
      hosts = await call('Hosts')
    } finally {
      hostsBusy = false
    }
  }

  let mail = $state(null)
  let mailBusy = $state(false)
  let mailError = $state('')

  async function installMail() {
    mailBusy = true
    mailError = ''
    try {
      mail = await call('InstallMail')
    } catch (e) {
      mailError = errorText(e)
    } finally {
      mailBusy = false
    }
  }

  let mailEnv = $derived(mail ? `MAIL_MAILER=smtp\nMAIL_HOST=127.0.0.1\nMAIL_PORT=${mail.smtpPort}\nMAIL_USERNAME=null\nMAIL_PASSWORD=null\nMAIL_ENCRYPTION=null` : '')

  async function load() {
    mail = await call('Mail')
    hosts = await call('Hosts')
    appearance = await call('Appearance')
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
    <h3>Appearance</h3>
    <div class="segmented" role="radiogroup" aria-label="Appearance">
      {#each modes as m}
        <button role="radio" aria-checked={appearance === m.id} class:on={appearance === m.id} onclick={() => setAppearance(m.id)}>{m.label}</button>
      {/each}
    </div>
    <p class="muted note">System follows the Windows light or dark setting.</p>
  </section>

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
    <h3>Mail capture</h3>
    {#if !mail}
      <p class="muted">Loading…</p>
    {:else if !mail.installed}
      <p>Catch every email your sites send, such as password resets and sign-ups, in a local inbox instead of sending it to real people.</p>
      <button class="primary" disabled={mailBusy} onclick={installMail}>{mailBusy ? 'Installing Mailpit…' : 'Install mail capture'}</button>
    {:else}
      <dl>
        <dt>Inbox</dt><dd><button class="link" onclick={() => run('', 'OpenURL', mail.url)}>{mail.url}</button></dd>
        <dt>SMTP</dt><dd class="mono">127.0.0.1:{mail.smtpPort}</dd>
      </dl>
      <p class="muted note">PHP's <code>mail()</code> (WordPress, plain PHP) already goes here. Laravel and Symfony use their own mail settings:</p>
      <div class="row">
        <button onclick={() => copy(mailEnv, 'mail')}>{copied === 'mail' ? 'Copied' : 'Copy Laravel .env settings'}</button>
        <button onclick={() => copy(`MAILER_DSN=smtp://127.0.0.1:${mail.smtpPort}`, 'symfony')}>{copied === 'symfony' ? 'Copied' : 'Copy Symfony MAILER_DSN'}</button>
      </div>
    {/if}
    {#if mailError}<pre class="err">{mailError}</pre>{/if}
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
    <h3>Site names (hosts file)</h3>
    {#if !hosts}
      <p class="muted">Loading…</p>
    {:else}
      <p>
        <span class="dot {hosts.inSync ? 'ok' : 'warn'}"></span>
        {#if hosts.wanted.length === 0}No sites to map yet.
        {:else if hosts.inSync}All {hosts.wanted.length} site names point to this computer.
        {:else}Some site names are not mapped yet, so browsers cannot find them.{/if}
      </p>
      {#if hosts.wanted.length}<p class="mono muted small">{hosts.wanted.join(', ')}</p>{/if}
      <div class="row">
        <button class="primary" disabled={hostsBusy || hosts.inSync} onclick={syncHosts}>
          {hostsBusy ? 'Waiting for the Windows prompt…' : hosts.inSync ? 'Up to date' : 'Update hosts file'}
        </button>
      </div>
      <p class="muted note">Windows asks for administrator permission once per change. {hosts.manual ? 'Automatic updates are off (manual_hosts).' : 'Ebur updates it after Start.'}</p>
      {#if hostsError || hosts.lastNote}<pre class="err">{hostsError || hosts.lastNote}</pre>{/if}
    {/if}
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
  .segmented { display: inline-flex; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
  .segmented button { border: none; border-radius: 0; padding: 7px 16px; background: var(--surface); }
  .segmented button + button { border-left: 1px solid var(--border); }
  .segmented button.on { background: var(--accent); color: var(--accent-text); font-weight: 600; }
  .note { margin: 10px 0 0; font-size: 13px; }
  .small { font-size: 12px; }
</style>
