import { mount } from 'svelte'
// Bundled fonts (SRS 4.5): embedded in the app, never loaded from the network.
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './app.css'
import App from './App.svelte'
import { call } from './api.js'
import { applyTheme } from './theme.js'

// Apply the saved appearance before the first paint where possible.
call('Appearance')
  .then(applyTheme)
  .catch(() => {})
  .finally(() => mount(App, { target: document.getElementById('app') }))
