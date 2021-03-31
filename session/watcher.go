package session

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/etcd/clientv3/concurrency"
)

// Watcher maintains a concurrency.Session with etcd and issues
// context.Context(s) tied to the lifetime of the session.
//
// These contexts are very useful to pass to etcd Watch(es), as
// the Watch will be canceled when the session is interrupted.
//
// A Watcher must not be copied after construction.
type Watcher struct {
	sync.Mutex
	online  int32 //atomic bool
	cancels []context.CancelFunc
}

// Online blocks until the Watcher reports itself
// as online or the provided ctx is canceled.
func (sw *Watcher) Online(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if sw.online == 1 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// WithOnline returns a context bound to the life cycle of the underling etcd.Session and
// branched off the provided ctx.
//
// The caller should immediately check the returned context.Context's error field
// per the usual error checking idiom to understand if etcd is alive.
//
// When the provided ctx is canceled the retured ctx is canceled as well.
//
// When the Watcher determines the etcd Session has disconnected the returned
// context will be canceled.
//
//
// Finally, calling the returned context.CancelFunc will cancel the returned context.
//
// Ensure either the provided context.Context is canceled, or the returned context.CancelFunc
// is called to not leak goroutines.
func (sw *Watcher) WithOnline(ctx context.Context) (context.Context, context.CancelFunc) {
	sw.Lock()
	defer sw.Unlock()
	wCtx, wCancel := context.WithCancel(ctx)

	// if we are currently offline,
	// cancel the context we just created
	// so caller is immediately aware
	// once control is returned.
	if sw.online == 0 {
		wCancel()
		return wCtx, wCancel
	}

	// arrange context propagation.
	go func() {
		select {
		case <-ctx.Done():
			wCancel()
		case <-wCtx.Done():
		}
	}()

	sw.cancels = append(sw.cancels, wCancel)
	return wCtx, wCancel
}

// NewWatcher constructions a session.Watcher.
//
// It is legal for the provided concurrency.Session to be offline when
// the constructor is called.
func NewWatcher(ctx context.Context, session *concurrency.Session) *Watcher {
	sw := &Watcher{}

	// do we have a dead session?
	// if yes the watch goroutine will
	// immediately enter reconnect.
	select {
	case <-session.Done():
		sw.online = 0
	default:
		sw.online = 1
	}

	go sw.watch(ctx, session)
	return sw
}

// Watch begins monitoring the concurrency.Session and
// issues a reconect if a session disruption takes place.
func (sw *Watcher) watch(ctx context.Context, session *concurrency.Session) error {
	select {
	case <-session.Done():
		sw.Lock()
		log.Printf("[SessionWatcher] detected session timeout with etcd.")
		atomic.CompareAndSwapInt32(&sw.online, 1, 0)
		sw.killContexts()
		sw.Unlock()
		go sw.reconnect(ctx, session)
		return nil
	case <-ctx.Done():
		// shutdown the watcher
		sw.Lock()
		log.Printf("[SessionWatcher] context canceled. gracefully shutting down.")
		atomic.CompareAndSwapInt32(&sw.online, 1, 0)
		sw.killContexts()
		sw.Unlock()
		return ctx.Err()
	}
}

// Reconnect is entered when the provided session is broken, indicating
// the etcd cluster cannot be reached.
//
// Reconnect will block until a new session is established or the provided
// ctx is canceled.
func (sw *Watcher) reconnect(ctx context.Context, session *concurrency.Session) error {
	for {
		if ctx.Err() != nil {
			log.Printf("[SessionWatcher] context canceled while reconnecting.")
			return ctx.Err()
		}
		c := session.Client()
		log.Printf("[SessionWatcher] attempting session reconnect")
		newSession, err := concurrency.NewSession(c, concurrency.WithContext(ctx))
		if err == nil {
			atomic.CompareAndSwapInt32(&sw.online, 0, 1)
			log.Printf("[SessionWatcher] session reconnected")
			go sw.watch(ctx, newSession)
			return nil
		}
	}
}

func (sw *Watcher) killContexts() {
	for _, cancel := range sw.cancels {
		cancel()
	}
	// allocate new, do not reslice, as we
	// want old cancel funcs to be gc'd.
	sw.cancels = make([]context.CancelFunc, 0)
}
