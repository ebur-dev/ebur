<script>
  import NewProject from './NewProject.svelte'

  let { ov, run } = $props()
  let creating = $state(false)

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
            <td><button class="link" onclick={() => run('', 'OpenURL', s.url)}>{s.url}</button></td>
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
  .small { font-size: 12px; font-family: var(--font-ui); }
  select { font-size: 13px; padding: 3px 6px; }
</style>
