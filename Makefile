containerName="etcd-v3.4.15"
image="quay.io/coreos/etcd:v3.4.15"

.PHONY: etcd-up
etcd-up:
	docker run -d -p 12379:2379 -p 12380:2380 --name $(containerName) $(image)\
		/usr/local/bin/etcd \
		--name s1 \
		--listen-client-urls http://0.0.0.0:2379 \
		--advertise-client-urls http://0.0.0.0:2379 \
		--listen-peer-urls http://0.0.0.0:2380 \
		--initial-advertise-peer-urls http://0.0.0.0:2380 \
		--initial-cluster s1=http://0.0.0.0:2380 \
		--initial-cluster-token tkn \
		--initial-cluster-state new \
		--log-level debug \
		--logger zap \
		--log-outputs stderr
	docker exec $(containerName) /bin/sh -c "/usr/local/bin/etcdctl version"

.PHONY: etcd-down
etcd-down:
	-docker kill $(containerName)
	-docker rm $(containerName)

.PHONY: unit
unit:
	go test -count=1 -race ./...

.PHONY: unit-verbose
unit-verbose:
	go test -v -count=1 -race ./...

.PHONY: automated-integration
automated-integration:
	make etcd-down
	make etcd-up
	go test -count=1 -race -tags integration ./...

.PHONY: integration-verbose
integration-verbose:
	go test -v -count=1 -race -tags integration ./...

