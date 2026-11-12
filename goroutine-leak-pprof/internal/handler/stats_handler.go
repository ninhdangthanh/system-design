package handler

import (
	"net/http"
	"runtime"

	"github.com/gin-gonic/gin"

	"goroutine-leak-pprof/internal/config"
	"goroutine-leak-pprof/internal/service"
)

type StatsHandler struct {
	mode    config.Mode
	reports *service.ReportService
}

type runtimeStats struct {
	Mode            config.Mode `json:"mode"`
	Goroutines      int         `json:"goroutines"`
	HeapAllocMB     float64     `json:"heap_alloc_mb"`
	HeapInuseMB     float64     `json:"heap_inuse_mb"`
	HeapObjects     uint64      `json:"heap_objects"`
	TotalAllocMB    float64     `json:"total_alloc_mb"`
	SysMB           float64     `json:"sys_mb"`
	NumGC           uint32      `json:"num_gc"`
	ReportCacheSize int         `json:"report_cache_size"`
}

func NewStatsHandler(mode config.Mode, reports *service.ReportService) *StatsHandler {
	return &StatsHandler{mode: mode, reports: reports}
}

func (h *StatsHandler) Runtime(c *gin.Context) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	c.JSON(http.StatusOK, runtimeStats{
		Mode:            h.mode,
		Goroutines:      runtime.NumGoroutine(),
		HeapAllocMB:     toMB(mem.HeapAlloc),
		HeapInuseMB:     toMB(mem.HeapInuse),
		HeapObjects:     mem.HeapObjects,
		TotalAllocMB:    toMB(mem.TotalAlloc),
		SysMB:           toMB(mem.Sys),
		NumGC:           mem.NumGC,
		ReportCacheSize: h.reports.CacheSize(),
	})
}

func (h *StatsHandler) ForceGC(c *gin.Context) {
	runtime.GC()
	h.Runtime(c)
}

func toMB(bytes uint64) float64 {
	return float64(bytes) / (1 << 20)
}
