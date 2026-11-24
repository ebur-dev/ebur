import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { call } from './api.js'
import { applyTheme } from './theme.js'

// Apply the saved appearance before the first paint where possible.
call('Appearance')
  .then(applyTheme)
  .catch(() => {})
  .finally(() => mount(App, { target: document.getElementById('app') }))
