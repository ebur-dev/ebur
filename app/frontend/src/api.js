// Calls Go methods on the bound App service ("main.App.<Method>").
// The Wails runtime is served by the app itself at /wails/runtime.js.
let runtime

async function rt() {
  runtime ??= import(/* @vite-ignore */ '/wails/runtime.js')
  return runtime
}

export async function call(method, ...args) {
  const r = await rt()
  return r.Call.ByName('main.App.' + method, ...args)
}

// on listens for an event emitted by the Go side and returns a function
// that stops listening.
export async function on(name, callback) {
  const r = await rt()
  // Go's Emit(name, value) delivers value itself as ev.data.
  return r.Events.On(name, (ev) => callback(ev?.data))
}

// errorText turns a rejected call into a readable message. Go errors
// arrive as "message\nNext step: hint" (NFR-14).
export function errorText(err) {
  if (!err) return ''
  if (typeof err === 'string') return err
  return err.message || String(err)
}
