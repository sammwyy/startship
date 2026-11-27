package registry

import _ "embed"

//go:embed recipes.json
var Recipes []byte

//go:embed services.json
var Services []byte
