package consoleapi

import _ "embed"

// OpenAPI is the console REST contract shared by diffx-console and the console app.
//
//go:embed openapi.yaml
var OpenAPI []byte
