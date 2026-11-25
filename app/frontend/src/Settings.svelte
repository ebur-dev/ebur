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
  // SVC-06: start services when Ebur opens; open Ebur at Windows sign-in.
  let start = $state(null)
  let startError = $state('')
  async function setStartup(method, on) {
    startError = ''
    try {
      start = await call(method, on)
    } catch (e) {
      startError = errorText(e)
      start = await call('StartupSettings')
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

  // Site address ending (SITE-04).
  let tld = $state(null)
  let tldBusy = $state(false)
  let tldError = $state('')
  async function setTLD(value) {
    if (!tld || value === tld.tld) return
    tldBusy = true
    tldError = ''
    try {
      tld = await call('SetTLD', value)
    } catch (e) {
      tldError = errorText(e)
    } finally {
      tldBusy = false
    }
  }

  let redis = $state(null)
  let redisBusy = $state(false)
  let redisError = $state('')
  async function installRedis() {
    redisBusy = true
    redisError = ''
    try {
      redis = await call('InstallRedis')
    } catch (e) {
      redisError = errorText(e)
    } finally {
      redisBusy = false
    }
  }
  let redisEnv = $derived(redis ? `REDIS_CLIENT=phpredis\nREDIS_HOST=127.0.0.1\nREDIS_PASSWORD=null\nREDIS_PORT=${redis.port}` : '')

  let node = $state(null)
  let nodeBusy = $state('')
  let nodeError = $state('')

  async function nodeAction(method, arg, label) {
    nodeBusy = label
    nodeError = ''
    try {
      node = await call(method, arg)
    } catch (e) {
      nodeError = errorText(e)
      node = await call('Node')
    } finally {
      nodeBusy = ''
    }
  }
  // The newest LTS line that is still supported: the one to suggest.
  let nodeSuggested = $derived(node?.available.find((v) => !v.legacy))

  let mailEnv = $derived(mail ? `MAIL_MAILER=smtp\nMAIL_HOST=127.0.0.1\nMAIL_PORT=${mail.smtpPort}\nMAIL_USERNAME=null\nMAIL_PASSWORD=null\nMAIL_ENCRYPTION=null` : '')

  async function load() {
    mail = await call('Mail')
    node = await call('Node')
    redis = await call('Redis')
    hosts = await call('Hosts')
    appearance = await call('Appearance')
    start = await call('StartupSettings')
    tld = await call('SiteAddresses')
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

  // .env for the server new projects use (each server has its own port).
  let envLine = $derived.by(() => {
    const s = db?.servers.find((x) => x.default) ?? db?.servers[0]
    return s ? `DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=${s.port}\nDB_USERNAME=${db.user}\nDB_PASSWORD=${db.password}` : ''
  })

  let dbBusy = $state('')
  let dbError = $state('')
  let mysqlPick = $state('')
  let mysqlInstalled = $derived(db ? db.mysql.filter((o) => o.installed) : [])
  let mysqlAvailable = $derived(db ? db.mysql.filter((o) => !o.installed) : [])
  $effect(() => {
    if (mysqlAvailable.length && !mysqlAvailable.some((o) => o.series === mysqlPick)) mysqlPick = mysqlAvailable[0].series
  })
  async function dbAction(method, arg, label) {
    dbBusy = label
    dbError = ''
    try {
      db = await (arg === undefined ? call(method) : call(method, arg))
    } catch (e) {
      dbError = errorText(e)
      db = await call('Database')
    } finally {
      dbBusy = ''
    }
  }
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
    <h3>Startup</h3>
    {#if !start}
      <p class="muted">Loading…</p>
    {:else}
      <label class="toggle">
        <input type="checkbox" checked={start.startServices} onchange={(e) => setStartup('SetStartServices', e.currentTarget.checked)}>
        <span>Start services when Ebur opens</span>
      </label>
      <label class="toggle">
        <input type="checkbox" checked={start.openAtSignIn} onchange={(e) => setStartup('SetOpenAtSignIn', e.currentTarget.checked)}>
        <span>Open Ebur when I sign in to Windows</span>
      </label>
      <p class="muted note">With both on, your sites are ready after every restart. Ebur opens in the tray, without its window.</p>
      {#if startError}<pre class="err">{startError}</pre>{/if}
    {/if}
  </section>

  <section class="card">
    <h3>Site addresses</h3>
    {#if !tld}
      <p class="muted">Loading…</p>
    {:else}
      <div class="segmented" role="radiogroup" aria-label="Site address ending">
        {#each ['test', 'localhost'] as t}
          <button role="radio" aria-checked={tld.tld === t} class:on={tld.tld === t} disabled={tldBusy} onclick={() => setTLD(t)}>.{t}</button>
        {/each}
      </div>
      {#if tld.tld === 'localhost'}
        <p class="muted note">Sites open at <code>https://&lt;name&gt;.localhost</code>. Browsers send these names to this computer by themselves, so the hosts file is not needed. Limit: PHP code that calls its own site's address (WordPress scheduled tasks and Site Health, a Laravel app calling its own URL) cannot reach it.</p>
      {:else}
        <p class="muted note">Sites open at <code>https://&lt;name&gt;.test</code>, mapped in the Windows hosts file (one administrator prompt per change).{tld.hostsBlocked ? ' The hosts file looks locked by security software on this PC: .localhost works without it.' : ''}</p>
      {/if}
      {#if tld.wordpress.length}
        <p class="warn note">WordPress keeps its address in its database: after switching, update {tld.wordpress.join(', ')} in WordPress (Settings &gt; General) or with <code>wp search-replace</code>.</p>
      {/if}
      {#if tldBusy}<p class="muted note">Restarting services on the new addresses…</p>{/if}
      {#if tldError}<pre class="err">{tldError}</pre>{/if}
    {/if}
  </section>

  <section class="card">
    <h3>Database</h3>
    {#if !db}
      <p class="muted">Loading…</p>
    {:else}
      {#if db.servers.length === 0}
        <p class="muted">No database server installed yet.</p>
      {/if}
      <ul class="servers">
        {#each db.servers as s}
          <li>
            <strong>{s.name}</strong> <span class="mono muted">127.0.0.1:{s.port}</span>
            {#if s.default && db.servers.length > 1}<span class="tag">new projects</span>{/if}
            {#if s.eol}<div class="warn small">End of life since {s.eol}: for old projects only.</div>{/if}
          </li>
        {/each}
      </ul>
      {#if db.servers.length > 1}
        <label class="pick">
          <span class="muted">New projects use</span>
          <select value={db.servers.find((s) => s.default)?.kind} disabled={!!dbBusy} onchange={(e) => dbAction('SetDefaultDB', e.currentTarget.value, 'Saving…')}>
            {#each db.servers as s}<option value={s.kind}>{s.name}</option>{/each}
          </select>
        </label>
      {/if}
      {#if mysqlInstalled.length > 1}
        <label class="pick">
          <span class="muted">MySQL version</span>
          <select value={db.servers.find((s) => s.kind === 'mysql')?.version} disabled={!!dbBusy} onchange={(e) => dbAction('SetMySQL', e.currentTarget.value, 'Switching MySQL…')}>
            {#each mysqlInstalled as o}<option value={o.version}>MySQL {o.version}{o.legacy ? ' (end of life)' : ''}</option>{/each}
          </select>
        </label>
      {/if}
      {#if !db.servers.some((s) => s.kind === 'mariadb')}
        <div class="row">
          <button disabled={!!dbBusy} onclick={() => dbAction('InstallMariaDB', undefined, 'Installing MariaDB (about 80 MB)…')}>{dbBusy || 'Install MariaDB'}</button>
        </div>
        <p class="muted note">MariaDB runs next to MySQL on its own port; projects choose one through DB_PORT in their .env.</p>
      {/if}
      {#if mysqlAvailable.length}
        <div class="row">
          <select bind:value={mysqlPick} disabled={!!dbBusy}>
            {#each mysqlAvailable as o}<option value={o.series}>MySQL {o.series}{o.legacy ? ` (end of life ${o.eol})` : ' LTS'}</option>{/each}
          </select>
          <button class={db.servers.some((s) => s.kind === 'mysql') ? '' : 'primary'} disabled={!!dbBusy || !mysqlPick} onclick={() => dbAction('InstallMySQL', mysqlPick, `Installing MySQL ${mysqlPick} (about 250-350 MB)…`)}>
            {dbBusy || 'Install MySQL'}
          </button>
        </div>
        <p class="muted note">MySQL runs next to MariaDB on its own port, so a project can match a MySQL production server. 5.7 needs Microsoft's Visual C++ 2013 runtime.</p>
      {/if}
      {#if dbError}<pre class="err">{dbError}</pre>{/if}
    {/if}
    {#if db && db.servers.length}
      <dl>
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
    <h3>Redis</h3>
    {#if !redis}
      <p class="muted">Loading…</p>
    {:else if !redis.installed}
      <p>A fast store for caching, sessions and queues (Laravel's <code>CACHE_STORE=redis</code>, <code>QUEUE_CONNECTION=redis</code>). Runs on this computer only, with PHP's redis extension.</p>
      <button class="primary" disabled={redisBusy} onclick={installRedis}>{redisBusy ? 'Installing Redis…' : 'Install Redis'}</button>
    {:else}
      <dl>
        <dt>Server</dt><dd>Redis {redis.version}</dd>
        <dt>Address</dt><dd class="mono">127.0.0.1:{redis.port}</dd>
        {#if redis.phpWith.length}<dt>PHP</dt><dd>extension in PHP {redis.phpWith.join(', ')}</dd>{/if}
      </dl>
      {#if redis.phpWithout.length}
        <p class="muted note">PHP {redis.phpWithout.join(', ')} has no redis extension yet. Laravel can use the pure-PHP client there: <code>composer require predis/predis</code> and <code>REDIS_CLIENT=predis</code>.</p>
      {/if}
      <div class="row"><button onclick={() => copy(redisEnv, 'redis')}>{copied === 'redis' ? 'Copied' : 'Copy Laravel .env settings'}</button></div>
    {/if}
    {#if redisError}<pre class="err">{redisError}</pre>{/if}
  </section>

  <section class="card">
    <h3>Node.js</h3>
    {#if !node}
      <p class="muted">Loading…</p>
    {:else}
      {#if node.installed.length === 0}
        <p>For front-end tools such as Vite (<code>npm run dev</code> in Laravel). Ebur puts Node on PATH only in its own terminal, so a Node you already have keeps working.</p>
      {:else}
        <label class="pick">
          <span class="muted">Default</span>
          <select value={node.default} disabled={!!nodeBusy} onchange={(e) => nodeAction('SetNode', e.currentTarget.value, 'Switching…')}>
            {#each node.installed as v}<option value={v.version}>{v.version}{v.legacy ? ' (end of life)' : ''}</option>{/each}
          </select>
        </label>
        <ul class="versions">
          {#each node.installed as v}
            <li>
              <span class="mono">{v.version}</span>
              <span class="muted small">{v.legacy ? `end of life since ${v.eol}` : `supported until ${v.eol}`}</span>
              {#if v.version !== node.default}
                <button class="link small" disabled={!!nodeBusy} onclick={() => nodeAction('RemoveNode', v.version, 'Removing…')}>Remove</button>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if node.available.length}
        <div class="row">
          {#if nodeSuggested}
            <button class={node.installed.length ? '' : 'primary'} disabled={!!nodeBusy} onclick={() => nodeAction('InstallNode', nodeSuggested.major, `Installing Node ${nodeSuggested.major}…`)}>
              {nodeBusy || `Install Node ${nodeSuggested.major} (LTS)`}
            </button>
          {/if}
          <select disabled={!!nodeBusy} value="" onchange={(e) => { const m = e.currentTarget.value; e.currentTarget.value = ''; if (m) nodeAction('InstallNode', m, `Installing Node ${m}…`) }}>
            <option value="">Other versions…</option>
            {#each node.available as v}<option value={v.major}>Node {v.major}{v.legacy ? ' (end of life, old projects)' : ''}</option>{/each}
          </select>
        </div>
      {/if}
      <p class="muted note">A site can choose its own version with <code>.nvmrc</code>, <code>engines.node</code> in package.json, or <code>ebur sites node &lt;site&gt; 22</code>.</p>
    {/if}
    {#if nodeError}<pre class="err">{nodeError}</pre>{/if}
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
  .toggle { display: flex; align-items: center; gap: 10px; padding: 4px 0; cursor: pointer; }
  .toggle input { width: 16px; height: 16px; margin: 0; accent-color: var(--accent); cursor: pointer; }
  .small { font-size: 12px; }
  .pick { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
  .servers { list-style: none; padding: 0; margin: 0 0 12px; display: grid; gap: 6px; }
  .tag { font-size: 11px; padding: 1px 7px; border-radius: 10px; background: color-mix(in srgb, var(--accent) 22%, transparent); margin-left: 6px; }
  .warn { color: var(--gold); }
  .versions { list-style: none; padding: 0; margin: 0 0 12px; display: grid; gap: 4px; }
  .versions li { display: flex; gap: 10px; align-items: baseline; flex-wrap: wrap; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
</style>
