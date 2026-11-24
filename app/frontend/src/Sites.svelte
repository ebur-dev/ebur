<script>
  let { ov, run } = $props()
</script>

<div class="row head">
  <h2>Sites</h2>
  <span class="muted">Every folder in <code>www\</code> is served at <code>&lt;folder&gt;.{ov.tld}</code></span>
  <span class="spacer"></span>
  <button onclick={() => run('', 'OpenWWW')}>Open www folder</button>
  <button onclick={() => run('', 'OpenTerminal')}>Open terminal</button>
</div>

{#if ov.sites.length === 0}
  <div class="card empty">
    <p><strong>No sites yet.</strong></p>
    <p class="muted">Create a folder in <code>{ov.root}\www</code>, for example <code>shop</code>, and it is served at <code>http://shop.{ov.tld}</code>. Laravel and Symfony <code>public\</code> folders are detected automatically.</p>
    <button class="primary" onclick={() => run('', 'OpenWWW')}>Open www folder</button>
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
            <td class="mono">{s.php}</td>
            <td><span class="dot {s.running ? 'running' : ''}"></span>{s.running ? 'Running' : 'Stopped'}</td>
            <td class="mono muted path" title={s.docRoot}>{s.docRoot}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if ov.adminer}
  <p class="muted">Database: <button class="link" onclick={() => run('', 'OpenURL', (ov.https ? 'https' : 'http') + '://db.' + ov.tld)}>Adminer at db.{ov.tld}</button> (credentials in Settings)</p>
{/if}

{#each ov.skipped as s}
  <p class="warn">Skipped <code>www\{s.folder}</code>: {s.reason}</p>
{/each}

<style>
  h2 { margin: 0; font-size: 18px; }
  .head { margin-bottom: 14px; }
  .spacer { flex: 1; }
  .flush { padding: 0; overflow: hidden; }
  .empty p { margin: 0 0 10px; }
  .path { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .warn { color: var(--gold); }
</style>
