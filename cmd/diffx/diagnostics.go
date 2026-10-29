package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/diagnostics"
)

func runDiagnostics(arguments []string, stdout io.Writer) error {
	const usage = "usage: diffx diagnostics export {REPORT_ID|--latest} --output FILE"
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h") {
		fmt.Fprintln(stdout, usage)
		return nil
	}
	if len(arguments) == 0 || arguments[0] != "export" {
		return errors.New(usage)
	}
	flags := flag.NewFlagSet("diffx diagnostics export", flag.ContinueOnError)
	flags.SetOutput(stdout)
	latest := flags.Bool("latest", false, "export the latest saved diagnostics report")
	output := flags.String("output", "", "new JSON output file; existing files are refused")
	flags.Usage = func() { fmt.Fprintln(stdout, usage); flags.PrintDefaults() }
	// Go flag stops at a positional ID. Normalize only this command's flags so
	// REPORT_ID --output FILE works without changing ordinary CLI option rules.
	var options, positionals []string
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			positionals = append(positionals, arguments[index+1:]...)
			break
		}
		if strings.HasPrefix(argument, "-") {
			options = append(options, argument)
			if (argument == "--output" || argument == "-output") && index+1 < len(arguments) {
				index++
				options = append(options, arguments[index])
			}
		} else {
			positionals = append(positionals, argument)
		}
	}
	if err := flags.Parse(append(options, positionals...)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 || (*latest && flags.NArg() != 0) || (!*latest && flags.NArg() != 1) {
		return errors.New("choose exactly one report ID or --latest")
	}
	if *output == "" {
		return errors.New("diagnostics export requires --output")
	}
	id := ""
	if flags.NArg() == 1 {
		id = flags.Arg(0)
	}
	directory, err := config.StateDir()
	if err != nil {
		return err
	}
	if err := diagnostics.Export(filepath.Join(directory, "diagnostics"), id, *latest, *output); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Diagnostics report exported")
	return nil
}
