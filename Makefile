CONFIG_PATH=${HOME}/.aphros
TAG ?= 0.0.4

.PHONY: build-docker
build-docker:
	docker build -t github.com/palsagnik/aphros:$(TAG) .

.PHONY: clean-conf
clean-conf:
	rm -rf ${CONFIG_PATH}

.PHONY: init
init:
	mkdir -p ${CONFIG_PATH}

.PHONY: gencert
gencert: init
	cfssl gencert \
		-initca test/certs/ca-csr.json | cfssljson -bare ca

	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/certs/ca-config.json \
		-profile=server \
		test/certs/server-csr.json | cfssljson -bare server
	
	cfssl gencert \
        -ca=ca.pem \
        -ca-key=ca-key.pem \
        -config=test/certs/ca-config.json \
        -profile=client \
        test/certs/client-csr.json | cfssljson -bare client
	
	cfssl gencert \
        -ca=ca.pem \
        -ca-key=ca-key.pem \
        -config=test/certs/ca-config.json \
        -profile=client \
        -cn="root" \
        test/certs/client-csr.json | cfssljson -bare root-client

	cfssl gencert \
		-ca=ca.pem \
		-ca-key=ca-key.pem \
		-config=test/certs/ca-config.json \
		-profile=client \
		-cn="nobody" \
		test/certs/client-csr.json | cfssljson -bare nobody-client


	mv *.pem *.csr ${CONFIG_PATH}
	
.PHONY: compile
compile:
	protoc api/v1/*.proto \
        --go_out=. \
        --go-grpc_out=. \
		--go_opt=paths=source_relative \
		--go-grpc_opt=paths=source_relative \
		--proto_path=.

$(CONFIG_PATH)/model.conf:
	cp test/acl/model.conf $(CONFIG_PATH)/model.conf

$(CONFIG_PATH)/policy.csv:
	cp test/acl/policy.csv $(CONFIG_PATH)/policy.csv

.PHONY: genacl
genacl: $(CONFIG_PATH)/policy.csv $(CONFIG_PATH)/model.conf
		echo "ACLs configured"

.PHONY: test
test:
	go test -race ./...

.PHONY: clean-chart
clean-chart:
	helm uninstall aphros
	kubectl delete pvc datadir-aphros-0 datadir-aphros-1 datadir-aphros-2

.PHONY: chart
chart:
	helm install aphros deploy/aphros

.PHONY: pods
pods:
	kubectl get pods