package pgstore

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// NotificationKind names what another (or this) replica committed.
type NotificationKind int

const (
	// ContextChanged: an observation binding or deletion of OwnerID's ContextID committed.
	ContextChanged NotificationKind = iota + 1
	// AccountRemoved: account OwnerID was disabled or deleted.
	AccountRemoved
	// JobQueued: an ingestion job became claimable.
	JobQueued
	// Resync: the session was lost and re-established; notifications
	// committed meanwhile were not delivered, so consumers re-read durable state.
	Resync
)

// Notification is a hint to query durable state, never a durable event.
type Notification struct {
	Kind      NotificationKind
	OwnerID   string
	ContextID string
}

const (
	listenRetryMin = 250 * time.Millisecond
	listenRetryMax = 5 * time.Second
	// listenIdle bounds how long a silently dead session goes unnoticed.
	listenIdle = 30 * time.Second
)

// Listener holds one dedicated session that LISTENs on every channel. Pooled
// connections return to the pool between statements and cannot hold LISTEN;
// neither can a transaction-mode PgBouncer, so the database URL must reach
// Postgres directly or through a session-mode pooler.
type Listener struct {
	config *pgx.ConnConfig
	mu     sync.Mutex
	conn   *pgx.Conn
}

// Listen opens the listening session before returning, so a composition that
// serves afterwards misses no commit while the session lasts.
func (store *Store) Listen(ctx context.Context) (*Listener, error) {
	listener := &Listener{config: store.config.ConnConfig}
	conn, err := listener.connect(ctx)
	if err != nil {
		return nil, err
	}
	listener.conn = conn
	return listener, nil
}

func (listener *Listener) connect(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, listener.config)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
			_ = conn.Close(context.Background())
			return nil, err
		}
	}
	return conn, nil
}

// Run delivers notifications until ctx ends, then closes the session. A lost
// session is logged and reopened with backoff; each reopening delivers Resync.
// deliver runs on Run's goroutine and must not block.
func (listener *Listener) Run(ctx context.Context, deliver func(Notification)) error {
	defer listener.Close()
	delay := listenRetryMin
	for {
		listener.mu.Lock()
		conn := listener.conn
		listener.mu.Unlock()
		if conn == nil {
			var err error
			conn, err = listener.connect(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				log.Printf("postgres listener: %v", err)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(delay):
				}
				delay = min(2*delay, listenRetryMax)
				continue
			}
			listener.mu.Lock()
			listener.conn = conn
			listener.mu.Unlock()
			delay = listenRetryMin
			deliver(Notification{Kind: Resync})
		}
		err := receive(ctx, conn, deliver)
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("postgres listener: session lost: %v", err)
		listener.Close()
	}
}

// receive returns when the session fails; idle periods are checked with a ping.
func receive(ctx context.Context, conn *pgx.Conn, deliver func(Notification)) error {
	for {
		wait, cancel := context.WithTimeout(ctx, listenIdle)
		notification, err := conn.WaitForNotification(wait)
		idle := wait.Err() != nil
		cancel()
		if err == nil {
			if value, ok := parseNotification(notification); ok {
				deliver(value)
			}
			continue
		}
		if ctx.Err() != nil || !idle {
			return err
		}
		ping, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = conn.Ping(ping)
		cancel()
		if err != nil {
			return err
		}
	}
}

func parseNotification(notification *pgconn.Notification) (Notification, bool) {
	switch notification.Channel {
	case contextChannel:
		owner, id, ok := strings.Cut(notification.Payload, " ")
		return Notification{Kind: ContextChanged, OwnerID: owner, ContextID: id}, ok && owner != "" && id != ""
	case accountChannel:
		return Notification{Kind: AccountRemoved, OwnerID: notification.Payload}, notification.Payload != ""
	case jobChannel:
		return Notification{Kind: JobQueued}, true
	}
	return Notification{}, false
}

// Close releases the session of a listener whose Run never started or has
// returned; it must not run concurrently with Run.
func (listener *Listener) Close() error {
	listener.mu.Lock()
	conn := listener.conn
	listener.conn = nil
	listener.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close(context.Background())
}
