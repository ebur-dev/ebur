// Package manifestdata embeds the runtime manifest shipped with this
// build. core/catalog prefers a newer, signed copy when one was fetched.
package manifestdata

import _ "embed"

//go:embed runtimes.json
var JSON []byte
