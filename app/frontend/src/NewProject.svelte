<script>
  // New project (PROJ-01/02): a name and a starter; Ebur makes the folder,
  // the database and the settings file, like `ebur new`.
  import { onMount, onDestroy } from 'svelte'
  import { call, on, errorText } from './api.js'

  let { ov, onclose } = $props()

  const kinds = [
    { id: 'laravel', label: 'Laravel', note: 'Latest Laravel via Composer; database set in .env and migrated.' },
    { id: 'wordpress', label: 'WordPress', note: 'Latest WordPress; database set in wp-config.php. Finish the install in the browser.' },
    { id: 'symfony', label: 'Symfony', note: 'Symfony skeleton via Composer; database set in .env.local.' },
    { id: 'blank', label: 'Blank', note: 'An empty site with an index.php. No database.' },
  ]

  let page = $state('form') // form | working | done
  let name = $state('')
  let kind = $state('laravel')
  let php = $state('')
  let error = $state('')
  let step = $state('')
  let detail = $state('')
  let result = $state(null)
  let offs = []

  const nameOK = $derived(/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(name))
  const taken = $derived(ov.sites.some((s) => s.name === name) || name === 'db' || name === 'mail')
  const needsDB = $derived(kind !== 'blank')
  const noDB = $derived(needsDB && !ov.dbInstalled)

  onMount(async () => {
    offs.push(await on('project:progress', (p) => { step = p?.step ?? ''; detail = p?.detail ?? '' }))
    offs.push(
      await on('project:finished', (r) => {
        if (r?.ok) {
          result = r.result
          page = 'done'
        } else {
          error = r?.error || 'Creating the project failed.'
          page = 'form'
        }
      }),
    )
  })
  onDestroy(() => offs.forEach((off) => off?.()))

  async function create() {
    error = ''
    step = 'Starting'
    detail = ''
    page = 'working'
    try {
      await call('CreateProject', name, kind, php)
    } catch (e) {
      error = errorText(e)
      page = 'form'
    }
  }
</script>

<div class="overlay" role="dialog" aria-modal="true" aria-labelledby="np-title">
  <div class="card panel">
    {#if page === 'form'}
      <h2 id="np-title">New project</h2>
      <label class="field">
        <span>Name</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={name} placeholder="shop" spellcheck="false" autofocus
          oninput={(e) => (name = e.currentTarget.value.toLowerCase())} />
        <small class="muted">
          {#if name && !nameOK}Use lowercase letters, digits and hyphens (not at the start or end).
          {:else if taken}A site named {name} already exists or is reserved.
          {:else}Served at <code>{name || 'name'}.{ov.tld}</code>, files in <code>www\{name || 'name'}</code>.{/if}
        </small>
      </label>

      <fieldset>
        <legend>Starter</legend>
        {#each kinds as k}
          <label class="kind" class:on={kind === k.id}>
            <input type="radio" bind:group={kind} value={k.id} />
            <span><strong>{k.label}</strong><br /><small class="muted">{k.note}</small></span>
          </label>
        {/each}
      </fieldset>

      <label class="field">
        <span>PHP</span>
        <select bind:value={php}>
          <option value="">Default ({ov.phpActive})</option>
          {#each ov.phpInstalled as v}<option value={v}>{v}{ov.phpLegacy.includes(v) ? ' (end of life)' : ''}</option>{/each}
        </select>
      </label>

      {#if noDB}<p class="warn">This starter needs MariaDB. Install it from the Services tab first, or choose Blank.</p>{/if}
      {#if error}<pre class="err">{error}</pre>{/if}

      <div class="actions">
        <button onclick={onclose}>Cancel</button>
        <button class="primary" disabled={!nameOK || taken || noDB} onclick={create}>Create</button>
      </div>
    {:else if page === 'working'}
      <h2 id="np-title">Creating {name}…</h2>
      <p class="step"><span class="spinner"></span>{step}</p>
      <p class="detail mono muted" title={detail}>{detail || ' '}</p>
      <p class="muted small">Downloads can take a minute or two. If you cancel, nothing is kept.</p>
      <div class="actions"><button onclick={() => call('CancelProject')}>Cancel</button></div>
    {:else}
      <h2 id="np-title">{result.name} is ready</h2>
      {#each result.next as n}<p>{n}</p>{/each}
      {#if result.database}<p class="muted small">Database <code>{result.database}</code> (login in Settings).</p>{/if}
      <div class="actions">
        <button onclick={onclose}>Close</button>
        <button class="primary" onclick={() => { call('OpenURL', result.url); onclose() }}>Open {result.url}</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 10; display: grid; place-items: center; padding: 16px;
    background: color-mix(in srgb, var(--ink) 45%, transparent);
  }
  .panel { width: min(520px, 100%); max-height: 100%; overflow: auto; padding: 20px 22px; }
  h2 { margin: 0 0 14px; font-size: 18px; }
  .field { display: grid; gap: 4px; margin-bottom: 14px; }
  .field > span, legend { font-weight: 600; font-size: 13px; }
  input:not([type]) {
    font: inherit; padding: 6px 9px; border-radius: 6px; border: 1px solid var(--border);
    background: var(--surface); color: var(--text);
  }
  input:not([type]):focus { outline: 2px solid var(--accent); outline-offset: -1px; }
  fieldset { border: none; padding: 0; margin: 0 0 14px; display: grid; gap: 6px; }
  legend { margin-bottom: 6px; padding: 0; }
  .kind { display: flex; gap: 10px; align-items: flex-start; padding: 8px 10px; border: 1px solid var(--border); border-radius: 8px; cursor: pointer; }
  .kind.on { border-color: var(--accent); background: color-mix(in srgb, var(--accent) 8%, var(--surface)); }
  .kind input { margin-top: 3px; accent-color: var(--accent); }
  .actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
  .warn { color: var(--gold); font-size: 13px; }
  .err { white-space: pre-wrap; font-family: var(--font-ui); font-size: 13px; color: var(--red); margin: 0; }
  .step { display: flex; align-items: center; gap: 10px; margin: 0 0 6px; }
  .detail { font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin: 0 0 12px; }
  .small { font-size: 12px; }
  .spinner {
    width: 14px; height: 14px; border-radius: 50%; flex: none;
    border: 2px solid var(--border); border-top-color: var(--accent); animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
