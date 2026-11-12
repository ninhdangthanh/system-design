# Goroutine leak + CPU/RAM profiling với Gin và pprof

Lab mô phỏng một **Order API (Gin)** có bug giống production: goroutine leak, CPU cao, RAM tăng mãi. Mục tiêu là tự tay **reproduce → quan sát → dùng `pprof` tìm đúng dòng code → fix → so sánh**.

"API profiling" mà bạn nhớ chính là **`net/http/pprof`** — package chuẩn của Go, expose các endpoint `/debug/pprof/*`. Với Gin dùng bản bọc sẵn **`github.com/gin-contrib/pprof`** (`pprof.Register(router)`), bên trong gọi y hệt `net/http/pprof`.

---

## 1. Kiến trúc

```
              :8080 (public)                          :9090
 loadgen ───► Gin API server ───────────────────────► fake upstream
              ├─ GET  /api/v1/orders/:id      ──► GET  /inventory/:id  (chậm 50–500ms)
              ├─ POST /api/v1/orders/checkout ──► POST /payments       (30% trả 500)
              └─ GET  /api/v1/reports/sales   (CPU + cache trong RAM)

              127.0.0.1:6060 (internal, chỉ bind localhost)
              ├─ /debug/pprof/*   (gin-contrib/pprof)
              ├─ GET  /debug/stats  (số goroutine, heap, cache size)
              └─ POST /debug/gc     (ép GC rồi trả stats)
```

Một binary, chạy được 2 mode qua env `APP_MODE`:

| Mode | Ý nghĩa |
|---|---|
| `leaky` (mặc định) | Code có bug |
| `fixed` | Cùng code path, đã sửa |

Cùng 1 loadgen, cùng 1 upstream → so sánh 2 mode là thấy ngay khác biệt.

### Cấu trúc thư mục

```
goroutine-leak-pprof/
├── cmd/
│   ├── server/     API server (Gin) + debug server (pprof)
│   ├── upstream/   fake inventory + payment service
│   └── loadgen/    tool bắn tải (khỏi cài hey/wrk/vegeta)
├── internal/
│   ├── config/     đọc env
│   ├── client/     HTTP client gọi upstream (inventory, payment)
│   ├── service/    business logic — nơi chứa bug
│   ├── handler/    Gin handler
│   └── server/     router, 2 http.Server, graceful shutdown
├── Makefile
└── profiles/       (tự sinh) file profile lưu ở đây
```

### Tại sao pprof chạy port riêng, chỉ bind 127.0.0.1?

Giống hệ thống thật: `/debug/pprof` lộ stack trace, cấu trúc code, và `profile?seconds=60` tốn CPU — **không bao giờ expose ra public**. Production thường để port riêng, chỉ truy cập qua `kubectl port-forward`, VPN, hoặc sidecar.

---

## 2. Các bug được cài sẵn

### Bug 1 — Goroutine leak: unbuffered channel + timeout
`internal/service/order_service.go` → `GetOrder`

```go
results := make(chan stockResult)        // leaky: unbuffered
go func() {
    stock, err := s.inventory.GetStock(ctx, productID)
    results <- stockResult{stock, err}   // kẹt mãi ở đây
}()
select {
case r := <-results: ...
case <-ctx.Done(): return ErrInventoryTimeout   // handler bỏ đi, không ai nhận nữa
}
```

Inventory chậm hơn timeout 200ms → handler trả 504 và return. Goroutine con sau đó gửi vào channel **không còn ai đọc** → block vĩnh viễn ở trạng thái `chan send`.

**Fix:** `make(chan stockResult, 1)` — buffer 1 slot, goroutine con gửi xong là thoát dù không ai đọc.

### Bug 2 — Goroutine leak: consumer chờ channel không bao giờ close (+ giữ RAM)
`order_service.go` → `Checkout` / `track`

Mỗi checkout spawn 1 goroutine `track` để theo dõi trạng thái đơn, `for { select { case s, ok := <-updates ... } }`, có `time.Ticker` heartbeat và giữ 1 buffer 128KB. Bản leaky **không bao giờ `close(updates)`** → goroutine sống mãi, ticker vẫn chạy, 128KB không được GC.

**Fix:** `defer close(updates)` (hàm `finishTracking`). Goroutine nhận `ok == false` và return. Ngoài ra goroutine còn lắng nghe `rootCtx.Done()` để thoát khi shutdown.

> Bài học: goroutine nào được start cũng phải trả lời được câu hỏi **"nó sẽ kết thúc khi nào, do ai báo?"**

### Bug 3 — Goroutine leak: không close `resp.Body`
`internal/client/payment_client.go` → `handleChargeResponseLeaky`

```go
if resp.StatusCode != http.StatusOK {
    return fmt.Errorf(...)        // quên resp.Body.Close()
}
defer resp.Body.Close()
```

Chỉ close body ở happy path. Khi payment trả 500, connection TCP bị giữ, `http.Transport` không tái sử dụng được → mỗi lần lỗi mở connection mới, mỗi connection tốn **2 goroutine** `persistConn.readLoop` + `persistConn.writeLoop` (và 1 file descriptor).

Vì sao không tự hết? Service gọi payment bằng `context.WithoutCancel(ctx)` (pattern thật: "đã charge thì không được huỷ giữa chừng khi client ngắt kết nối"). Context không bao giờ cancel → không gì đóng connection.

**Fix:** `defer drainAndClose(resp.Body)` ngay sau khi `Do()` thành công, trước mọi `if`. Drain (`io.Copy(io.Discard, ...)`) để connection quay lại pool.

### Bug 4 — CPU cao
`internal/service/report_service.go` → `buildSlow`

- `regexp.MustCompile` **bên trong vòng lặp** 6000 dòng.
- `csv += fmt.Sprintf(...)` → copy chuỗi O(n²).

**Fix (`buildFast`):** compile regex 1 lần ở package level, `strings.HasPrefix` lọc trước, dùng `strings.Builder`.

### Bug 5 — RAM tăng mãi (memory leak không phải goroutine)
`report_service.go` → `cacheKey`

Cache map không giới hạn, key là **toàn bộ raw query**. Frontend gửi kèm `_ts=<timestamp>` (cache-busting) → mỗi request 1 key mới → map phình mãi, không bao giờ hit.

**Fix:** key chỉ theo tham số có nghĩa (`day`). Production nên dùng cache có TTL + giới hạn size (LRU).

---

## 3. Chuẩn bị

```bash
cd goroutine-leak-pprof
make deps
make build
brew install graphviz   # tuỳ chọn: để xem dạng Graph trong pprof web UI. Flame Graph không cần.
```

Go ≥ 1.26 (dependency của Gin yêu cầu).

---

## 4. Lab từng bước

Mở 3–4 terminal trong folder `goroutine-leak-pprof/`.

| Terminal | Lệnh |
|---|---|
| T1 | `make upstream` |
| T2 | `make run-leaky` |
| T3 | `make watch` (in stats mỗi 2s) |
| T4 | chạy load + profiling |

### Bước 1 — Baseline

```bash
make stats
# {"mode":"leaky","goroutines":8,"heap_alloc_mb":1.6, ... "report_cache_size":0}
```

### Bước 2 — Bắn tải, nhìn goroutine tăng mà không giảm

```bash
make load D=15s
```

Kết quả mẫu (máy M-series):

```
requests: 3972  rps: 261.6
status: 200=2651 402=415 504=906
```

Sau khi load **dừng hẳn**, đợi vài giây rồi:

```bash
make gc
# {"mode":"leaky","goroutines":3082,"heap_alloc_mb":414, ... "report_cache_size":1324}
```

Không còn request nào mà vẫn 3082 goroutine, đã ép GC mà heap vẫn ~400MB → **leak**. Dấu hiệu y hệt trên Grafana production: goroutine count và memory đi lên theo bậc thang, không về baseline sau khi traffic giảm.

### Bước 3 — Goroutine profile: leak ở đâu?

**3a. Đếm theo trạng thái (nhanh nhất, chỉ cần curl):**

```bash
make goroutine-dump-full
```
```
2162 [select]        ← track (1324) + persistConn readLoop/writeLoop
 906 [chan send]     ← đúng bằng số request 504 ở trên
  10 [IO wait]
```
`debug=2` in từng goroutine kèm stack đầy đủ và **thời gian bị block** (vd `[chan send, 3 minutes]`) — goroutine block lâu bất thường là nghi phạm số 1. File lưu ở `profiles/`.

**3b. Gộp theo stack (`debug=1`):**

```bash
make goroutine-dump
```
Dòng đầu: `goroutine profile: total 3082`. Sau đó mỗi block là "N @ địa chỉ" + stack: N goroutine có cùng stack. Block có N lớn nhất là chỗ leak.

**3c. Top function giữ goroutine:**

```bash
make goroutine-top
```
```
 cum    cum%
 906  29.40%  service.(*OrderService).GetOrder.func1     ← Bug 1
1324  42.96%  service.(*OrderService).track              ← Bug 2
 422  13.69%  net/http.(*persistConn).readLoop           ← Bug 3
 422  13.69%  net/http.(*persistConn).writeLoop          ← Bug 3
```

Đọc ngược: `persistConn` leak + số lượng ≈ số request 402 (payment fail) → nghĩ ngay tới **body không close ở nhánh lỗi**.

**3d. Web UI:**

```bash
make goroutine-web      # mở http://localhost:7070
```
View → **Flame Graph** hoặc **Top**. Click vào node để xem source.

### Bước 4 — RAM profiling

```bash
make heap-snapshot           # snapshot đầu tiên = base
make load D=15s
make heap-snapshot           # snapshot sau
make heap-diff               # chỉ hiện phần TĂNG THÊM giữa 2 lần
make heap-top                # trạng thái hiện tại
```
```
     flat  flat%
 333.57MB 78.84%  service.(*OrderService).track       ← buffer trong goroutine leak
  76.51MB 18.08%  service.(*ReportService).buildSlow   ← report bị nhét vào cache không giới hạn
```

`heap-diff` là cách chuẩn để tìm memory leak: so 2 thời điểm, cái gì tăng liên tục là nghi phạm. `heap?gc=1` ép GC trước khi chụp để loại rác chưa dọn.

Phân biệt 2 sample index:

| Index | Ý nghĩa | Dùng khi |
|---|---|---|
| `inuse_space` (`make heap-web`) | Bộ nhớ **đang giữ** | Tìm memory leak |
| `alloc_space` (`make allocs-web`) | Tổng đã **cấp phát** từ lúc start (kể cả đã GC) | Tìm code allocate nhiều → GC chạy nhiều → tốn CPU |

> Goroutine leak = memory leak: mỗi goroutine tốn ≥ 2KB stack, cộng mọi thứ nó tham chiếu (ở đây 128KB buffer) đều không được GC.

### Bước 5 — CPU profiling

```bash
make cpu-under-load SECONDS=20
```
Lệnh này bắn tải vào `/reports/sales` và đồng thời gọi `/debug/pprof/profile?seconds=20`. Sau đó:

```bash
make cpu-top     # -cum, chỉ focus code của mình
make cpu-list    # CPU theo TỪNG DÒNG code
make cpu-web     # Flame Graph
```

`cpu-list` cho kết quả rất rõ:
```
     .     23.16s    106:  matcher := regexp.MustCompile(salesLinePattern)
  10ms      1.19s    107:  line, ok := parseSalesLine(matcher, raw)
     .      2.97s    113:  csv += fmt.Sprintf(...)
```
→ 85% CPU nằm ở việc compile lại regex mỗi vòng lặp.

Đọc `flat` vs `cum`:
- **flat**: thời gian chạy *trong chính* hàm đó.
- **cum**: flat + mọi hàm nó gọi. Tìm "hàm nghiệp vụ nào đắt" thì sort theo `cum`.

Trong leaky mode bạn sẽ thấy `runtime.usleep`, `pthread_cond_wait`, `runtime.lock2` chiếm nhiều `flat` — đó là GC và scheduler phải xử lý heap lớn + hàng nghìn goroutine/ticker leak. Leak làm **cả hệ thống** chậm, không chỉ endpoint có bug.

### Bước 6 — So sánh với bản fixed

`Ctrl+C` server ở T2, rồi:

```bash
make run-fixed
```
Lặp lại Bước 2–5. Kết quả mẫu cùng tải 15s:

| | leaky | fixed |
|---|---|---|
| Throughput | 262 rps | 393 rps |
| Avg latency | 114ms | 76ms |
| Goroutine sau load | 3082 | 22 |
| Heap sau GC | ~414MB | ~2MB |
| Report cache size | 1324 | ≤ 3 |

### Bước 7 — Bắt leak trong unit test bằng `goleak`

Production-grade: leak phải bị chặn ở CI, không đợi lên prod mới thấy.

```bash
make test        # LEAK_MODE=fixed  → PASS
make test-leak   # LEAK_MODE=leaky  → FAIL và in ra goroutine leak
```
```
found unexpected goroutines:
[Goroutine 8 in state chan send, with ...(*OrderService).GetOrder.func1 ...
[Goroutine 98 in state select, with ...(*OrderService).track ...
```
`go.uber.org/goleak` so sánh goroutine trước/sau test. Cách dùng phổ biến: `defer goleak.VerifyNone(t)` trong từng test hoặc `goleak.VerifyTestMain(m)` cho cả package.

---

## 5. Các endpoint pprof

Tất cả dưới `http://127.0.0.1:6060/debug/pprof/`:

| Endpoint | Dùng để |
|---|---|
| `/` | Trang index, liệt kê mọi profile |
| `goroutine?debug=1` | Goroutine gộp theo stack (text) |
| `goroutine?debug=2` | Từng goroutine, stack đầy đủ + thời gian block |
| `goroutine` | Dạng binary cho `go tool pprof` |
| `heap` / `heap?gc=1` | Bộ nhớ đang dùng (inuse) + đã cấp phát |
| `allocs` | Giống heap, mặc định view `alloc_space` |
| `profile?seconds=N` | CPU profile N giây (blocking, chạy lúc có tải) |
| `block` | Thời gian goroutine bị block (chan, select, mutex…) — cần `runtime.SetBlockProfileRate` |
| `mutex` | Lock contention — cần `runtime.SetMutexProfileFraction` |
| `threadcreate` | OS thread được tạo |
| `trace?seconds=N` | Execution trace, xem bằng `go tool trace` (`make trace`) |

Server này đã bật `SetBlockProfileRate` và `SetMutexProfileFraction` trong `internal/server/server.go`.

Lệnh `go tool pprof` hay dùng trong interactive mode (`go tool pprof <url>`): `top`, `top -cum`, `list <regex>`, `peek <regex>`, `web`, `traces`.

---

## 6. Playbook khi gặp trên production

1. **Phát hiện**: metric `go_goroutines` (Prometheus client tự export) và `process_resident_memory_bytes` tăng theo bậc thang, không về baseline khi traffic giảm.
2. **Thu thập** (port-forward vào pod):
   ```bash
   kubectl port-forward pod/<pod> 6060:6060
   curl -s localhost:6060/debug/pprof/goroutine?debug=1 > g1.txt
   # đợi 10–15 phút
   curl -s localhost:6060/debug/pprof/goroutine?debug=1 > g2.txt
   ```
   So 2 file: stack nào có N tăng là stack leak. Heap làm tương tự rồi `go tool pprof -base heap1 heap2`.
3. **Đọc trạng thái**: `chan send`/`chan receive` lâu → channel không ai đọc/không ai close; `select` → thiếu `ctx.Done()`; `IO wait` hoặc `persistConn` → connection/body không close; `semacquire` → deadlock/mutex.
4. **Fix + chặn tái phát**: thêm `goleak` vào test, review checklist: *mỗi `go func` có điều kiện dừng chưa? mỗi `resp.Body` có close ở mọi nhánh chưa? mỗi `time.NewTicker` có `Stop()` chưa?*
5. **Continuous profiling** (tuỳ chọn): Pyroscope / Grafana Pyroscope, Datadog Continuous Profiler, GCP Cloud Profiler — chụp profile định kỳ để so sánh theo thời gian.

---

## 7. Makefile

```bash
make help
```

| Nhóm | Target |
|---|---|
| Setup | `deps`, `build`, `fmt`, `vet`, `clean` |
| Run | `upstream`, `run-leaky`, `run-fixed` |
| Load (`SCENARIO=all\|orders\|checkout\|reports C=30 D=30s`) | `load`, `load-orders`, `load-checkout`, `load-reports` |
| Quan sát | `stats`, `watch`, `gc` |
| Goroutine | `goroutine-count`, `goroutine-dump`, `goroutine-dump-full`, `goroutine-top`, `goroutine-web` |
| RAM | `heap-snapshot`, `heap-diff`, `heap-top`, `heap-web`, `allocs-web` |
| CPU (`SECONDS=20`) | `cpu-under-load`, `cpu-profile`, `cpu-top`, `cpu-list`, `cpu-web` |
| Khác | `block-web`, `mutex-web`, `trace`, `test`, `test-leak` |

Mọi profile được lưu vào `./profiles` (kể cả file `go tool pprof` tự lưu, nhờ `PPROF_TMPDIR`). Web UI mở ở port `PPROF_PORT=7070`.

### Biến môi trường của server

| Biến | Mặc định |
|---|---|
| `APP_MODE` | `leaky` |
| `APP_ADDR` | `:8080` |
| `DEBUG_ADDR` | `127.0.0.1:6060` |
| `UPSTREAM_URL` | `http://localhost:9090` |
| `INVENTORY_TIMEOUT` | `200ms` |
| `SHUTDOWN_TIMEOUT` | `10s` |
