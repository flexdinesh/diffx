package remoteserver

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/flexdinesh/diffx/internal/pushservice"
)

type pushDeviceJSON struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	CreatedAt     int64  `json:"createdAt"`
	LastSuccessAt *int64 `json:"lastSuccessAt"`
}

func projectDevice(device pushservice.Subscription) pushDeviceJSON {
	return pushDeviceJSON{ID: device.ID, Label: device.Label, CreatedAt: device.CreatedAt, LastSuccessAt: device.LastSuccessAt}
}

// pushRoutes serves Web Push settings and devices. Like account routes they
// need a browser session: a subscription receives the account's notifications.
func (remote *transport) pushRoutes(w http.ResponseWriter, r *http.Request, p principal) {
	ctx, owner := r.Context(), p.account.ID
	if r.URL.Path == "/api/v2/push" {
		if r.Method != http.MethodGet {
			problem(w, 405, "Method not allowed")
			return
		}
		settings, err := remote.push.Settings(ctx, owner)
		if err != nil {
			pushResult(w, err)
			return
		}
		devices := make([]pushDeviceJSON, 0, len(settings.Devices))
		for _, device := range settings.Devices {
			devices = append(devices, projectDevice(device))
		}
		writeJSON(w, 200, struct {
			Enabled   bool             `json:"enabled"`
			PublicKey string           `json:"publicKey,omitempty"`
			Devices   []pushDeviceJSON `json:"devices"`
		}{settings.Enabled, settings.PublicKey, devices})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v2/push/")
	if path == "subscriptions" {
		if r.Method != http.MethodPut {
			problem(w, 405, "Method not allowed")
			return
		}
		var input struct {
			Endpoint string `json:"endpoint"`
			Keys     struct {
				P256dh string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
			Label string `json:"label"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		device, err := remote.push.Subscribe(ctx, owner, pushservice.SubscriptionInput{Endpoint: input.Endpoint, P256dh: input.Keys.P256dh, Auth: input.Keys.Auth, Label: input.Label})
		if err != nil {
			pushResult(w, err)
			return
		}
		writeJSON(w, 200, projectDevice(device))
		return
	}
	id, rest, _ := strings.Cut(strings.TrimPrefix(path, "subscriptions/"), "/")
	if !strings.HasPrefix(path, "subscriptions/") || id == "" {
		http.NotFound(w, r)
		return
	}
	switch {
	case rest == "" && r.Method == http.MethodDelete:
		pushResult(w, remote.push.Unsubscribe(ctx, owner, id))
	case rest == "test" && r.Method == http.MethodPost:
		pushResult(w, remote.push.Test(ctx, owner, id))
	case rest == "" || rest == "test":
		problem(w, 405, "Method not allowed")
	default:
		http.NotFound(w, r)
	}
}

func pushResult(w http.ResponseWriter, err error) {
	var limited *pushservice.RateLimitedError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, pushservice.ErrDisabled):
		problem(w, 409, pushservice.ErrDisabled.Error())
	case errors.Is(err, pushservice.ErrNotFound):
		problem(w, 404, "Push subscription not found")
	case errors.Is(err, pushservice.ErrInvalid):
		problem(w, 400, err.Error())
	case errors.As(err, &limited):
		seconds := int64(limited.RetryAfter.Seconds())
		if seconds < 1 {
			seconds = 60
		}
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		problem(w, 429, "Push service rate limit reached; try again later")
	case errors.Is(err, pushservice.ErrRejected):
		log.Printf("push test: %v", err)
		problem(w, 502, "Push service did not accept the notification")
	default:
		log.Printf("push: %v", err)
		problem(w, 500, "Server could not process request")
	}
}
