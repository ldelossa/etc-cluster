package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/cloudscaleorg/etc-cluster/events"
	"github.com/cloudscaleorg/etc-cluster/session"
	etcd "go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/concurrency"
)

var (
	endpoint = flag.String("h", "localhost:2379", "the etcd host to connect to.")
	prefix   = flag.String("p", "/keys", "the prefix to source events from")
)

func main() {
	flag.Parse()

	// signal handler
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		cancel()
	}()

	conf := etcd.Config{
		Endpoints: []string{*endpoint},
	}
	client, err := etcd.New(conf)
	if err != nil {
		log.Fatal(err)
	}
	f := func(e *etcd.Event) {
		log.Printf("received event %+v\n", e)
	}
	sess, err := concurrency.NewSession(client, concurrency.WithTTL(5))
	if err != nil {
		log.Fatalf("could not create session: %v", err)
	}
	sw := session.NewWatcher(ctx, sess)
	es := events.NewEventSource(client.Watcher, sw, *prefix, f)
	err = es.Source(ctx)
	log.Printf("%v", err)

}
