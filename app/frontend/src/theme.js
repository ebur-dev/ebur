// Appearance (GUI-05): "system" follows Windows; "light" and "dark" fix
// it. The choice is saved in ebur.json by the Go side.
export function applyTheme(mode) {
  const root = document.documentElement
  if (mode === 'light' || mode === 'dark') root.dataset.theme = mode
  else delete root.dataset.theme
}
