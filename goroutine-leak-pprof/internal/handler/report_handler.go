package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"goroutine-leak-pprof/internal/service"
)

type ReportHandler struct {
	reports *service.ReportService
}

func NewReportHandler(reports *service.ReportService) *ReportHandler {
	return &ReportHandler{reports: reports}
}

func (h *ReportHandler) SalesReport(c *gin.Context) {
	query := service.ReportQuery{
		Day:      c.DefaultQuery("day", "2026-11-05"),
		RawQuery: c.Request.URL.RawQuery,
	}
	c.JSON(http.StatusOK, h.reports.SalesReport(query))
}
