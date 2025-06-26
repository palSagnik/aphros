CONFIG_PATH=${HOME}/.aphros
TAG ?= 0.0.4
NAMESPACE ?= default
RELEASE_NAME ?= aphros
LOCAL_PORT_RPC ?= 8400
LOCAL_PORT_SERF ?= 8401

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

.PHONY: help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build & Development:"
	@echo "  build-docker       Build Docker image"
	@echo "  compile            Compile protobuf files"
	@echo "  test               Run tests"
	@echo ""
	@echo "Certificate Management:"
	@echo "  gencert            Generate TLS certificates"
	@echo "  genacl             Generate ACL configuration"
	@echo "  clean-conf         Clean configuration directory"
	@echo ""
	@echo "Kubernetes Deployment:"
	@echo "  chart              Deploy Helm chart"
	@echo "  clean-chart        Remove Helm chart and PVCs"
	@echo "  pods               Show pods"
	@echo ""
	@echo "Jump Server (Port Forwarding):"
	@echo "  jumpserver-help    Show jump server commands"
	@echo "  jumpserver-deploy  Deploy jump server"
	@echo "  jumpserver-start   Start port forwarding"
	@echo "  jumpserver-stop    Stop port forwarding"

.PHONY: jumpserver-deploy
jumpserver-deploy:
	@echo "Deploying socat jump server..."
	helm upgrade --install $(RELEASE_NAME) deploy/aphros --namespace $(NAMESPACE) --create-namespace
	@echo "Waiting for jump server to be ready..."
	kubectl wait --for=condition=Ready pod -l "app.kubernetes.io/component=jumpserver" -n $(NAMESPACE) --timeout=120s
	@echo "Jump server deployed successfully!"

.PHONY: jumpserver-start
jumpserver-start: jumpserver-cleanup
	@echo "Starting port forwarding through socat jump server..."
	@if ! kubectl get pod -l "app.kubernetes.io/component=jumpserver" -n $(NAMESPACE) --no-headers 2>/dev/null | grep -q Running; then \
		echo "Jump server is not ready. Deploy it first with: make jumpserver-deploy"; \
		exit 1; \
	fi
	@POD_NAME=$$(kubectl get pods -n $(NAMESPACE) -l "app.kubernetes.io/component=jumpserver" -o name | head -1 | cut -d/ -f2); \
	echo "Using jump server pod: $$POD_NAME"; \
	echo "Forwarding local ports:"; \
	echo "  $(LOCAL_PORT_RPC) -> jump server -> aphros-0 RPC (leader)"; \
	echo "  $(LOCAL_PORT_SERF) -> jump server -> aphros-0 Serf (leader)"; \
	kubectl port-forward -n $(NAMESPACE) pod/$$POD_NAME $(LOCAL_PORT_RPC):8400 $(LOCAL_PORT_SERF):8401 &

.PHONY: jumpserver-stop
jumpserver-stop:
	@echo "Stopping port forwarding..."
	@if pgrep -f "kubectl.*port-forward" >/dev/null; then \
		pkill -f "kubectl.*port-forward" && echo "Port forwarding stopped"; \
		sleep 1; \
		if pgrep -f "kubectl.*port-forward" >/dev/null; then \
			echo "Force killing remaining processes..."; \
			pkill -9 -f "kubectl.*port-forward"; \
		fi \
	else \
		echo "No port forwarding processes found"; \
	fi

.PHONY: jumpserver-cleanup
jumpserver-cleanup:
	@echo "Cleaning up any existing port forwarding..."
	@pkill -f "kubectl.*port-forward" 2>/dev/null || true
	@sleep 1

.PHONY: jumpserver-status
jumpserver-status:
	@echo "Checking jump server status..."
	@POD_NAME=$$(kubectl get pods -n $(NAMESPACE) -l "app.kubernetes.io/component=jumpserver" -o name 2>/dev/null | head -1 | cut -d/ -f2); \
	if [ -z "$$POD_NAME" ]; then \
		echo "Jump server pod not found"; \
		exit 1; \
	fi; \
	echo "Pod: $$POD_NAME"; \
	kubectl get pod $$POD_NAME -n $(NAMESPACE) -o wide

.PHONY: jumpserver-logs
jumpserver-logs:
	@POD_NAME=$$(kubectl get pods -n $(NAMESPACE) -l "app.kubernetes.io/component=jumpserver" -o name 2>/dev/null | head -1 | cut -d/ -f2); \
	if [ -z "$$POD_NAME" ]; then \
		echo "Jump server pod not found"; \
		exit 1; \
	fi; \
	echo "Showing logs for jump server pod: $$POD_NAME"; \
	kubectl logs -n $(NAMESPACE) $$POD_NAME -f

.PHONY: jumpserver-test
jumpserver-test:
	@echo "Testing connection through jump server..."
	@if ! pgrep -f "kubectl.*port-forward" >/dev/null; then \
		echo "Port forwarding not detected. Start it first with: make jumpserver-start"; \
		exit 1; \
	fi
	@echo "Testing RPC connection..."
	@if command -v go >/dev/null 2>&1; then \
		echo "Running getservers command..."; \
		go run cmd/getservers/main.go -addr "localhost:$(LOCAL_PORT_RPC)"; \
	else \
		echo "Go not available, testing TCP connection..."; \
		if command -v nc >/dev/null 2>&1; then \
			echo "test" | nc -w 5 localhost $(LOCAL_PORT_RPC) && echo "RPC port is reachable" || echo "RPC port not reachable"; \
		else \
			echo "nc not available for connection test"; \
		fi \
	fi

.PHONY: jumpserver-help
jumpserver-help:
	@echo "Jump Server Management Commands:"
	@echo ""
	@echo "  make jumpserver-deploy     Deploy the jump server to Kubernetes"
	@echo "  make jumpserver-start      Start port forwarding through jump server"
	@echo "  make jumpserver-stop       Stop port forwarding"
	@echo "  make jumpserver-status     Check jump server status"
	@echo "  make jumpserver-logs       Show jump server logs"
	@echo "  make jumpserver-test       Test connectivity through jump server"
	@echo "  make jumpserver-cleanup    Clean up any existing port forwarding processes"
	@echo ""
	@echo "Configuration (can be overridden):"
	@echo "  NAMESPACE=$(NAMESPACE)"
	@echo "  RELEASE_NAME=$(RELEASE_NAME)"
	@echo "  LOCAL_PORT_RPC=$(LOCAL_PORT_RPC)"
	@echo "  LOCAL_PORT_SERF=$(LOCAL_PORT_SERF)"
	@echo ""
	@echo "Examples:"
	@echo "  make jumpserver-deploy"
	@echo "  make jumpserver-start"
	@echo "  make jumpserver-start NAMESPACE=production LOCAL_PORT_RPC=9400"
	@echo "  make jumpserver-test"
	@echo "  make jumpserver-stop"