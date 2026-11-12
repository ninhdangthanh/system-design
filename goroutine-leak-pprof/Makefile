APP_URL      ?= http://localhost:8080
DEBUG_URL    ?= http://127.0.0.1:6060
PPROF_URL    := $(DEBUG_URL)/debug/pprof
PROFILE_DIR  := profiles
BIN_DIR      := bin
SCENARIO     ?= all
C            ?= 30
D            ?= 30s
SECONDS      ?= 20
PPROF_PORT   ?= 7070
TS           := $(shell date +%H%M%S)

export PPROF_TMPDIR := $(CURDIR)/$(PROFILE_DIR)

.PHONY: help deps build fmt vet clean \
        upstream run-leaky run-fixed \
        load load-orders load-checkout load-reports \
        stats watch gc \
        goroutine-count goroutine-dump goroutine-dump-full goroutine-top goroutine-web \
        heap-snapshot heap-top heap-web heap-diff allocs-web \
        cpu-profile cpu-top cpu-list cpu-web cpu-under-load \
        block-web mutex-web trace \
        test test-leak

help:
	@echo "Setup"
	@echo "  make deps                 tai dependencies"
	@echo "  make build                build server, upstream, loadgen vao ./bin"
	@echo ""
	@echo "Chay he thong (moi lenh 1 terminal)"
	@echo "  make upstream             fake inventory + payment service :9090"
	@echo "  make run-leaky            API server co bug (leak goroutine, CPU, RAM) :8080, pprof :6060"
	@echo "  make run-fixed            API server da fix"
	@echo ""
	@echo "Ban tai  (SCENARIO=all|orders|checkout|reports C=30 D=30s)"
	@echo "  make load | load-orders | load-checkout | load-reports"
	@echo ""
	@echo "Quan sat"
	@echo "  make stats | watch | gc   so goroutine + heap realtime"
	@echo ""
	@echo "Goroutine profiling"
	@echo "  make goroutine-count      tong so goroutine theo pprof"
	@echo "  make goroutine-dump       dump gop theo stack (debug=1)"
	@echo "  make goroutine-dump-full  dump tung goroutine + thoi gian bi block (debug=2)"
	@echo "  make goroutine-top        top function giu goroutine"
	@echo "  make goroutine-web        mo pprof web UI"
	@echo ""
	@echo "RAM profiling"
	@echo "  make heap-snapshot        luu heap profile vao ./profiles"
	@echo "  make heap-top | heap-web  xem inuse_space"
	@echo "  make heap-diff            so sanh 2 heap snapshot (base vs latest)"
	@echo "  make allocs-web           xem tong luong allocate (alloc_space)"
	@echo ""
	@echo "CPU profiling  (SECONDS=20)"
	@echo "  make cpu-under-load       vua ban tai vua thu CPU profile, roi mo web UI"
	@echo "  make cpu-profile | cpu-top | cpu-list | cpu-web"
	@echo ""
	@echo "Khac"
	@echo "  make block-web | mutex-web | trace"
	@echo "  make test                 goleak test (mode fixed -> PASS)"
	@echo "  make test-leak            goleak test (mode leaky -> FAIL, in ra goroutine leak)"

deps:
	go mod download

build:
	go build -o $(BIN_DIR)/server ./cmd/server
	go build -o $(BIN_DIR)/upstream ./cmd/upstream
	go build -o $(BIN_DIR)/loadgen ./cmd/loadgen

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf $(BIN_DIR) $(PROFILE_DIR)

upstream:
	go run ./cmd/upstream

run-leaky: build
	APP_MODE=leaky ./$(BIN_DIR)/server

run-fixed: build
	APP_MODE=fixed ./$(BIN_DIR)/server

load:
	go run ./cmd/loadgen -base $(APP_URL) -scenario $(SCENARIO) -c $(C) -d $(D)

load-orders:
	$(MAKE) load SCENARIO=orders

load-checkout:
	$(MAKE) load SCENARIO=checkout

load-reports:
	$(MAKE) load SCENARIO=reports

stats:
	@curl -s $(DEBUG_URL)/debug/stats; echo

watch:
	@while true; do curl -s $(DEBUG_URL)/debug/stats; echo; sleep 2; done

gc:
	@curl -s -X POST $(DEBUG_URL)/debug/gc; echo

$(PROFILE_DIR):
	@mkdir -p $(PROFILE_DIR)

goroutine-count:
	@curl -s "$(PPROF_URL)/goroutine?debug=1" | head -1

goroutine-dump: | $(PROFILE_DIR)
	curl -s "$(PPROF_URL)/goroutine?debug=1" -o $(PROFILE_DIR)/goroutine-$(TS).txt
	@echo "saved $(PROFILE_DIR)/goroutine-$(TS).txt"
	@head -40 $(PROFILE_DIR)/goroutine-$(TS).txt

goroutine-dump-full: | $(PROFILE_DIR)
	curl -s "$(PPROF_URL)/goroutine?debug=2" -o $(PROFILE_DIR)/goroutine-full-$(TS).txt
	@echo "saved $(PROFILE_DIR)/goroutine-full-$(TS).txt"
	@grep -Eo "^goroutine [0-9]+ \[[^]]+\]" $(PROFILE_DIR)/goroutine-full-$(TS).txt | sed -E 's/goroutine [0-9]+ //' | sort | uniq -c | sort -rn | head -15

goroutine-top:
	go tool pprof -top -nodecount=20 "$(PPROF_URL)/goroutine"

goroutine-web:
	go tool pprof -http=:$(PPROF_PORT) "$(PPROF_URL)/goroutine"

heap-snapshot: | $(PROFILE_DIR)
	curl -s "$(PPROF_URL)/heap?gc=1" -o $(PROFILE_DIR)/heap-$(TS).pb.gz
	@cp $(PROFILE_DIR)/heap-$(TS).pb.gz $(PROFILE_DIR)/heap-latest.pb.gz
	@test -f $(PROFILE_DIR)/heap-base.pb.gz || cp $(PROFILE_DIR)/heap-$(TS).pb.gz $(PROFILE_DIR)/heap-base.pb.gz
	@echo "saved $(PROFILE_DIR)/heap-$(TS).pb.gz"

heap-top:
	go tool pprof -top -sample_index=inuse_space -nodecount=20 "$(PPROF_URL)/heap"

heap-web:
	go tool pprof -http=:$(PPROF_PORT) -sample_index=inuse_space "$(PPROF_URL)/heap"

heap-diff:
	go tool pprof -top -sample_index=inuse_space -nodecount=20 -base $(PROFILE_DIR)/heap-base.pb.gz $(PROFILE_DIR)/heap-latest.pb.gz

allocs-web:
	go tool pprof -http=:$(PPROF_PORT) -sample_index=alloc_space "$(PPROF_URL)/allocs"

cpu-profile: | $(PROFILE_DIR)
	curl -s "$(PPROF_URL)/profile?seconds=$(SECONDS)" -o $(PROFILE_DIR)/cpu-$(TS).pb.gz
	@cp $(PROFILE_DIR)/cpu-$(TS).pb.gz $(PROFILE_DIR)/cpu-latest.pb.gz
	@echo "saved $(PROFILE_DIR)/cpu-$(TS).pb.gz"

cpu-top:
	go tool pprof -top -cum -focus=goroutine-leak-pprof -nodecount=30 $(PROFILE_DIR)/cpu-latest.pb.gz

cpu-list:
	go tool pprof -list='buildSlow|buildFast' $(PROFILE_DIR)/cpu-latest.pb.gz

cpu-web:
	go tool pprof -http=:$(PPROF_PORT) $(PROFILE_DIR)/cpu-latest.pb.gz

cpu-under-load: | $(PROFILE_DIR)
	@$(MAKE) --no-print-directory load SCENARIO=reports D=$$(( $(SECONDS) + 5 ))s & \
	sleep 2 && $(MAKE) --no-print-directory cpu-profile SECONDS=$(SECONDS) && wait
	$(MAKE) --no-print-directory cpu-top

block-web:
	go tool pprof -http=:$(PPROF_PORT) "$(PPROF_URL)/block"

mutex-web:
	go tool pprof -http=:$(PPROF_PORT) "$(PPROF_URL)/mutex"

trace: | $(PROFILE_DIR)
	curl -s "$(PPROF_URL)/trace?seconds=5" -o $(PROFILE_DIR)/trace-$(TS).out
	go tool trace $(PROFILE_DIR)/trace-$(TS).out

test:
	LEAK_MODE=fixed go test -count=1 -v ./...

test-leak:
	LEAK_MODE=leaky go test -count=1 -v ./internal/service/ || true
