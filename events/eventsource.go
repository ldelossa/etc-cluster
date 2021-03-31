package events

import (
	"context"
	"log"

	"github.com/cloudscaleorg/etc-cluster/session"
	etcd "go.etcd.io/etcd/clientv3"
)

type ReduceFunc func(*etcd.Event)

// EventSource mirrors the state of all values on a particular
// etcd kv prefix to a local state.
//
// EventSource uses a persistent etcd Watch to react to new events
// without polling (edge driven).
//
// When the EventSource receives an event via the Watch or obtains all
// objects on the prefix it will record each revision for each key it encounters.
//
// For all revisions never seen before it will call its ReduceFunc, a dependency
// injected function holding the instructions on building or modifying the local state.
//
// Once an EventSource is created it will attempt to maintain the etcd Watch
// and recover from any errors.
type EventSource struct {
	watch  etcd.Watcher
	sw     *session.Watcher
	prefix string
	f      ReduceFunc
	fence  map[string]int64
}

func NewEventSource(w etcd.Watcher, sw *session.Watcher, prefix string, f ReduceFunc) EventSource {
	return EventSource{
		watch:  w,
		prefix: prefix,
		sw:     sw,
		f:      f,
		fence:  make(map[string]int64),
	}
}

func (ev EventSource) Source(ctx context.Context) error {
Retry:
	// this is the only way this function terminates.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// if this error is actually due to the incoming context
	// being cancled, it will be handled at the top of loop.
	wCtx, wCancel := ev.sw.WithOnline(ctx)
	if wCtx.Err() != nil {
		log.Printf("[EventSource-%s] could not confirm etcd liveliness. waiting for etcd to come online.", ev.prefix)
		ev.sw.Online(ctx)
		goto Retry
	}

	// this watch will remain alive until either the wCtx indicates etcd host
	// not available or the incoming ctx is canceled.
	wc := ev.watch.Watch(wCtx, ev.prefix, etcd.WithPrefix(), etcd.WithRev(1))
	log.Printf("[EventSource-%s] watch established.", ev.prefix)
	for wr := range wc {
		if wr.Canceled {
			log.Printf("[EventSource-%s] watch canceled. error: %v. attempting reconnect", ev.prefix, wr.Err())
			goto Retry
		}
		for _, event := range wr.Events {
			ev.ProcessEvent(event)
		}
	}
	wCancel()
	goto Retry
}

// ProcessEvent determines whether an Event should be passed to
// the configured ReduceFunc.
//
// All events in etcd have a monotonically increasing revision id.
// By tracking the highest revision number we've seen for a particular
// key we can limit the number of times our ReduceFunc must be called.
//
// Tracking these revisions also allows for opportunistic concurrency
// when utilizing etcd transactions.
func (ev EventSource) ProcessEvent(event *etcd.Event) {
	key := string(event.Kv.Key)
	rev, ok := ev.fence[key]
	switch {
	case event.Kv.ModRevision <= rev:
	case !ok:
		fallthrough
	default:
		ev.fence[key] = event.Kv.ModRevision
		ev.f(event)
	}
	return
}
