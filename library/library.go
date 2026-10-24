// Package library embeds the curated agent definitions shipped with groot:
// the agents of the @tinchoram collection, the index that describes them for
// publication, and the list of public catalogs that were validated. The
// files are read by internal/library.
package library

import "embed"

// Agents holds agents/*.md, index.yaml and sources.yaml.
//
//go:embed agents/*.md index.yaml sources.yaml
var Agents embed.FS
