module github.com/flexdinesh/diffx/apps/desktop

go 1.27.3

require (
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.7.4
	github.com/flexdinesh/diffx v0.0.0
)

require (
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/flexdinesh/diffx => ../..

tool github.com/egoist/mygo/cmd/mygo
