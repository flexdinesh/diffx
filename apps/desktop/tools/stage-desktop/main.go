// stage-desktop stages the Vite frontend for the independent Go module.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: stage-desktop source destination")
		os.Exit(2)
	}
	if err := stage(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func stage(source, destination string) error {
	if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
		return fmt.Errorf("desktop build missing: %w", err)
	}
	if _, err := os.Stat(filepath.Join(source, "assets")); err != nil {
		return fmt.Errorf("desktop assets missing: %w", err)
	}
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(destination, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(destination, "placeholder.txt"), []byte("Staged desktop frontend.\n"), 0644); err != nil {
			return err
		}
	}
	if marker, err := os.Stat(filepath.Join(destination, "placeholder.txt")); err != nil || !marker.Mode().IsRegular() {
		return errors.New("refusing to replace unmarked desktop assets")
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "placeholder.txt" {
			if err := os.RemoveAll(filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
	}
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, name)
		if err != nil || rel == "." {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("desktop build contains symlink")
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
