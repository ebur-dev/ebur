// Package manifestdata embeds the runtime manifest shipped with this
// build. Fetching a newer signed copy at startup arrives with SEC-02/08.
package manifestdata

import _ "embed"

//go:embed runtimes.json
var JSON []byte
