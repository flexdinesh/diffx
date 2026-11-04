module github.com/flexdinesh/diffx/apps/desktop

go 1.27.3

require (
	github.com/egoist/mygo v0.7.4
	github.com/flexdinesh/diffx v0.0.0
	github.com/oapi-codegen/nullable v1.1.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/flexdinesh/diffx => ../..

tool github.com/egoist/mygo/cmd/mygo
