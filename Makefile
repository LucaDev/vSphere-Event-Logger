.PHONY: build container clean tools simulate

BINARY_NAME=vsphere-eventlogger
IMAGE_NAME=vsphere-eventlogger:latest

build:
	CGO_ENABLED=0 go build -o $(BINARY_NAME) main.go

container: build
	docker build -t $(IMAGE_NAME) .

clean:
	rm -f $(BINARY_NAME)

tools:
	@if ! command -v vcsim >/dev/null || ! command -v govc >/dev/null; then \
		echo "Installing vcsim and govc via git clone..."; \
		TEMP_DIR=$$(mktemp -d); \
		git clone --depth 1 https://github.com/vmware/govmomi.git $$TEMP_DIR/govmomi; \
		cd $$TEMP_DIR/govmomi/vcsim && go install .; \
		cd $$TEMP_DIR/govmomi/govc && go install .; \
		rm -rf $$TEMP_DIR; \
	fi

simulate: tools build
	@echo "Starting vcsim in the background..."
	@export PATH="$$PATH:$$(go env GOPATH)/bin"; \
	vcsim -l 127.0.0.1:8989 > /dev/null 2>&1 & VCSIM_PID=$$$$!; \
	trap "kill $$$$VCSIM_PID 2>/dev/null" EXIT INT TERM; \
	sleep 2; \
	echo "Triggering a background event in 3 seconds..."; \
	(sleep 3; GOVC_URL="https://user:pass@127.0.0.1:8989/sdk" GOVC_INSECURE="true" govc vm.power -off DC0_H0_VM0) & \
	echo "Starting event logger (press Ctrl+C to stop)..."; \
	GOVMOMI_URL="https://user:pass@127.0.0.1:8989/sdk" GOVMOMI_INSECURE="true" ./$(BINARY_NAME)

