<script>
  import NewProject from './NewProject.svelte'

  let { ov, run } = $props()
  let creating = $state(false)

  // Share links (TOOL-04).
  let sharing = $state('') // site whose share options are open
  let shareMinutes = $state(60)
  let sharePassword = $state(false)
  let copied = $state('')
  const shareOf = (name) => ov.status.shares?.find((x) => x.site === name)
  function startShare(name) {
    const label = ov.shareReady ? `Opening a public link for ${name}…` : 'Downloading cloudflared (about 53 MB), then opening the link…'
    sharing = ''
    run(label, 'ShareSite', name, shareMinutes, sharePassword)
  }
  async function copy(text, what) {
    await navigator.clipboard.writeText(text)
    copied = what
    setTimeout(() => (copied = ''), 1500)
  }
  function left(t) {
    const m = Math.max(0, Math.round((new Date(t) - Date.now()) / 60000))
    return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min left` : `${m} min left`
  }

  const sourceText = { default: 'default', '.ebur': '.ebur file', 'composer.json': 'composer.json' }

  // "" means the default version: the site's php line is removed.
  function choose(site, value) {
    run('Switching ' + site.name + ' to PHP ' + (value || ov.phpActive) + '…', 'SetSitePHP', site.name, value)
  }

  // The picker shows a pin only when the .ebur file set it.
  const pinned = (s) => (s.phpSource === '.ebur' && !s.phpProblem ? s.php : '')

  // The "no pin" choice names what actually runs: composer.json can pick
  // a version other than the default.
  const autoLabel = (s) =>
    s.phpSource === 'composer.json' && !s.phpProblem ? `Automatic: ${s.php}` : `Default (${ov.phpActive})`
</script>

<div class="row head">
  <h2>Sites</h2>
  <span class="muted">Every folder in <code>www\</code> is served at <code>&lt;folder&gt;.{ov.tld}</code></span>
  <span class="spacer"></span>
  <button class="primary" onclick={() => (creating = true)}>New project…</button>
  <button onclick={() => run('', 'LinkProject')} title="Serve a project kept outside www\ without moving it">Add existing project…</button>
  <button onclick={() => run('', 'OpenWWW')}>Open www folder</button>
  <button onclick={() => run('', 'OpenTerminal')}>Open terminal</button>
</div>

{#if ov.sites.length === 0}
  <div class="card empty">
    <p><strong>No sites yet.</strong></p>
    <p class="muted">Create a Laravel, WordPress or Symfony project with <strong>New project…</strong>, or create a folder in <code>{ov.root}\www</code>, for example <code>shop</code>, and it is served at <code>http://shop.{ov.tld}</code>. Laravel and Symfony <code>public\</code> folders are detected automatically.</p>
    <button class="primary" onclick={() => (creating = true)}>New project…</button>
    <button onclick={() => run('', 'OpenWWW')}>Open www folder</button>
  </div>
{:else}
  <div class="card flush">
    <table>
      <thead><tr><th>Site</th><th>Address</th><th>PHP</th><th>Status</th><th>Document root</th></tr></thead>
      <tbody>
        {#each ov.sites as s}
          <tr>
            <td><strong>{s.name}</strong></td>
            <td>
              <button class="link" onclick={() => run('', 'OpenURL', s.url)}>{s.url}</button>
              {#if shareOf(s.name)}
                {@const sh = shareOf(s.name)}
                <div class="share">
                  <span class="pub">Public:</span>
                  <button class="link" onclick={() => run('', 'OpenURL', sh.url)}>{sh.url.replace('https://', '')}</button>
                  <button class="link small" onclick={() => copy(sh.url + (sh.password ? `\nUser: ${sh.user}  Password: ${sh.password}` : ''), sh.site)}>{copied === sh.site ? 'Copied' : 'Copy'}</button>
                  <button class="link small" onclick={() => run(`Ending the share of ${sh.site}…`, 'UnshareSite', sh.site)}>Stop</button>
                  <div class="muted small">{left(sh.expires)}{sh.password ? ` · user ${sh.user}, password ${sh.password}` : ''}</div>
                </div>
              {:else if sharing === s.name}
                <div class="share-opts">
                  <label>For
                    <select bind:value={shareMinutes}>
                      <option value={30}>30 minutes</option><option value={60}>1 hour</option><option value={180}>3 hours</option><option value={480}>8 hours</option>
                    </select>
                  </label>
                  <label><input type="checkbox" bind:checked={sharePassword} /> Ask for a password</label>
                  <p class="muted small">Anyone with the link can open <strong>{s.name}</strong> (only this site) until it ends. Traffic passes through Cloudflare; debug pages are off for visitors.{ov.shareReady ? '' : ' The first share downloads cloudflared (about 53 MB).'}</p>
                  <button class="primary" onclick={() => startShare(s.name)}>Share</button>
                  <button onclick={() => (sharing = '')}>Cancel</button>
                </div>
              {:else if s.running}
                <button class="link small" onclick={() => { sharing = s.name; shareMinutes = 60; sharePassword = false }} title="Give this site a temporary public address">Share…</button>
              {/if}
            </td>
            <td>
              <select value={pinned(s)} onchange={(e) => choose(s, e.currentTarget.value)} title="PHP version for this site">
                <option value="">{autoLabel(s)}</option>
                {#each ov.phpInstalled as v}<option value={v}>{v}{ov.phpLegacy.includes(v) ? ' (end of life)' : ''}</option>{/each}
              </select>
              {#if s.phpSource === 'composer.json' && !s.phpProblem}
                <div class="src">from composer.json ({s.phpWant})</div>
              {:else if s.phpSource === '.ebur' && !s.phpProblem}
                <div class="src">pinned in .ebur</div>
              {/if}
              {#if s.phpProblem}<div class="problem" title={s.phpProblem}>{s.phpProblem}</div>{/if}
              {#if s.node}<div class="src" title="Node for npm in this site's Ebur terminal">Node {s.node}{s.nodeSource !== 'default' ? ` from ${s.nodeSource}` : ''}</div>{/if}
              {#if s.nodeProblem}<div class="problem" title={s.nodeProblem}>{s.nodeProblem}</div>{/if}
              {#if s.phpEol}<div class="problem" title="This PHP version gets no security fixes. Fine for maintaining an old project on this computer; plan to upgrade.">Legacy: end of life since {s.phpEol}</div>{/if}
            </td>
            <td><span class="dot {s.running ? 'running' : ''}"></span>{s.running ? 'Running' : 'Stopped'}</td>
            <td class="mono muted path" title={s.target || s.docRoot}>
              {#if s.target}
                <span class="linked">linked from</span> {s.target}
                <div><button class="link small" onclick={() => run('Unlinking ' + s.name + '…', 'UnlinkSite', s.name)} title="Stop serving this project. Its files are not touched.">Unlink</button></div>
              {:else}
                {s.docRoot}
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  <p class="muted note">Each site can use its own PHP version: pick one here, or let the project's <code>composer.json</code> decide. Picking a version writes a <code>.ebur</code> file in the site folder; commit it so your team gets the same version.</p>
{/if}

{#if ov.adminer}
  <p class="muted">Database: <button class="link" onclick={() => run('', 'OpenURL', (ov.https ? 'https' : 'http') + '://db.' + ov.tld)}>Adminer at db.{ov.tld}</button> (credentials in Settings)</p>
{/if}
{#if ov.mail}
  <p class="muted">Mail: everything your sites send is caught at <button class="link" onclick={() => run('', 'OpenURL', (ov.https ? 'https' : 'http') + '://mail.' + ov.tld)}>mail.{ov.tld}</button>, never delivered.</p>
{/if}

{#if creating}<NewProject {ov} onclose={() => (creating = false)} />{/if}

{#each ov.skipped as s}
  <p class="warn">Skipped <code>www\{s.folder}</code>: {s.reason}</p>
{/each}

<style>
  h2 { margin: 0; font-size: 18px; }
  .head { margin-bottom: 14px; }
  .spacer { flex: 1; }
  .flush { padding: 0; overflow: hidden; }
  .empty p { margin: 0 0 10px; }
  .path { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .warn { color: var(--gold); }
  .src { font-size: 12px; color: var(--muted); margin-top: 3px; }
  .problem { font-size: 12px; color: var(--gold); margin-top: 3px; max-width: 260px; }
  .note { font-size: 13px; margin-top: 10px; }
  .linked { font-family: var(--font-ui); color: var(--accent); }
  .share { margin-top: 4px; font-size: 13px; }
  .share .pub { color: var(--green); font-weight: 600; }
  .share-opts { margin-top: 6px; padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; display: grid; gap: 6px; max-width: 340px; }
  .share-opts label { display: flex; gap: 8px; align-items: center; font-size: 13px; }
  .share-opts p { margin: 0; }
  .small { font-size: 12px; font-family: var(--font-ui); }
  select { font-size: 13px; padding: 3px 6px; }
</style>
