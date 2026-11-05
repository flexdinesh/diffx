package main

import (
	"fmt"
	"github.com/flexdinesh/diffx/apps/desktop/internal/app"
	"os"
)

func main() {
	handled, err := app.HandleCommand(os.Args[1:], os.Stdout)
	if !handled && err == nil {
		err = app.Run(os.Args[1:], os.Stdin, os.Stderr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
