package main

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestConnectRejectsInvalidTargetsBeforeSSH(t *testing.T) {
	for args, want := range map[string]string{
		"":                          "one target",
		"devbox":                    "ssh://HOST",
		"ssh://-oProxyCommand=x":    "ssh://HOST",
		"ssh://a ssh://b":           "one target",
		"ssh://devbox --port 70000": "port must be",
	} {
		err := runConnect(t.Context(), strings.Fields(args), io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: %v", args, err)
		}
	}
}

func TestConnectFlagsMayFollowTheTarget(t *testing.T) {
	got := normalizeConnectArguments([]string{"ssh://devbox", "--port", "7995", "--json"})
	want := []string{"--port", "7995", "--json", "--", "ssh://devbox"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
