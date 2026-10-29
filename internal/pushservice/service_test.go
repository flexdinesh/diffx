package pushservice_test

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/flexdinesh/diffx/internal/pushservice"
	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/testsupport/pgtest"
)

func TestEnablementFollowsThePublicURLAndKeysPersist(t *testing.T) {
	store := pgtest.Store(t)
	owner := testsupport.RemoteAccount(t, store, "alice")
	for publicURL, enabled := range map[string]bool{
		"": false, "http://diffx.example": false, "ftp://diffx.example": false, "https://": false,
		"https://diffx.example": true, "http://localhost:7980": true, "http://127.0.0.1:7980": true, "http://[::1]:7980": true,
	} {
		service, err := pushservice.New(t.Context(), store.Push(), pushservice.Options{PublicURL: publicURL})
		if err != nil {
			t.Fatal(err)
		}
		settings, err := service.Settings(t.Context(), owner.ID)
		if err != nil || settings.Enabled != enabled || (settings.PublicKey != "") != enabled {
			t.Fatalf("%q: %+v %v", publicURL, settings, err)
		}
		if !enabled {
			device := testsupport.NewPushDevice(t)
			if _, err := service.Subscribe(t.Context(), owner.ID, pushservice.SubscriptionInput{Endpoint: "https://push.example/a", P256dh: device.P256dh(), Auth: device.AuthSecret()}); !errors.Is(err, pushservice.ErrDisabled) {
				t.Fatalf("%q subscribed: %v", publicURL, err)
			}
			if err := service.Test(t.Context(), owner.ID, "any"); !errors.Is(err, pushservice.ErrDisabled) {
				t.Fatalf("%q tested: %v", publicURL, err)
			}
		}
	}
	first, err := pushservice.New(t.Context(), store.Push(), pushservice.Options{PublicURL: "https://diffx.example"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := pushservice.New(t.Context(), store.Push(), pushservice.Options{PublicURL: "https://other.example"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := first.Settings(t.Context(), owner.ID)
	b, _ := second.Settings(t.Context(), owner.ID)
	point, err := base64.RawURLEncoding.DecodeString(a.PublicKey)
	if err != nil || a.PublicKey != b.PublicKey {
		t.Fatalf("VAPID keys rotated: %q %q", a.PublicKey, b.PublicKey)
	}
	if _, err := ecdh.P256().NewPublicKey(point); err != nil || len(point) != 65 {
		t.Fatalf("public key is not an uncompressed P-256 point: %v", err)
	}
}

func TestSubscribeValidatesBrowserSubscriptions(t *testing.T) {
	store := pgtest.Store(t)
	owner := testsupport.RemoteAccount(t, store, "alice")
	service, err := pushservice.New(t.Context(), store.Push(), pushservice.Options{PublicURL: "https://diffx.example"})
	if err != nil {
		t.Fatal(err)
	}
	device := testsupport.NewPushDevice(t)
	valid := pushservice.SubscriptionInput{Endpoint: "https://fcm.googleapis.com/fcm/send/abc", P256dh: device.P256dh(), Auth: device.AuthSecret(), Label: "Chrome on Android"}
	notOnCurve := append([]byte{4}, make([]byte, 64)...)
	for name, change := range map[string]func(*pushservice.SubscriptionInput){
		"plain http":  func(input *pushservice.SubscriptionInput) { input.Endpoint = "http://push.example/a" },
		"relative":    func(input *pushservice.SubscriptionInput) { input.Endpoint = "/push" },
		"credentials": func(input *pushservice.SubscriptionInput) { input.Endpoint = "https://user:pass@push.example/a" },
		"long endpoint": func(input *pushservice.SubscriptionInput) {
			input.Endpoint = "https://push.example/" + strings.Repeat("a", 2048)
		},
		"short p256dh": func(input *pushservice.SubscriptionInput) { input.P256dh = input.P256dh[:40] },
		"compressed": func(input *pushservice.SubscriptionInput) {
			input.P256dh = base64.RawURLEncoding.EncodeToString(append([]byte{2}, make([]byte, 32)...))
		},
		"off curve": func(input *pushservice.SubscriptionInput) {
			input.P256dh = base64.RawURLEncoding.EncodeToString(notOnCurve)
		},
		"standard base64": func(input *pushservice.SubscriptionInput) { input.Auth = "++++++++++++++++++++++" },
		"short auth": func(input *pushservice.SubscriptionInput) {
			input.Auth = base64.RawURLEncoding.EncodeToString(make([]byte, 8))
		},
		"long label": func(input *pushservice.SubscriptionInput) { input.Label = strings.Repeat("é", 121) },
	} {
		input := valid
		change(&input)
		if _, err := service.Subscribe(t.Context(), owner.ID, input); !errors.Is(err, pushservice.ErrInvalid) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	padded := valid
	padded.Auth += "=="
	padded.Label = strings.Repeat("é", 120)
	if _, err := service.Subscribe(t.Context(), owner.ID, padded); err != nil {
		t.Fatalf("padded keys and a 120 character label: %v", err)
	}
	loopback := valid
	loopback.Endpoint = "http://127.0.0.1:9/push"
	if _, err := service.Subscribe(t.Context(), owner.ID, loopback); !errors.Is(err, pushservice.ErrInvalid) {
		t.Fatalf("public server accepted a loopback http endpoint: %v", err)
	}
	local, err := pushservice.New(t.Context(), store.Push(), pushservice.Options{PublicURL: "http://localhost:7980"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := local.Subscribe(t.Context(), owner.ID, loopback); err != nil {
		t.Fatalf("loopback development endpoint: %v", err)
	}
}
