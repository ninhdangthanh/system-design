package main

import (
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

const paymentFailureRate = 0.3

func main() {
	gin.SetMode(gin.ReleaseMode)
	addr := ":9090"
	if value := os.Getenv("UPSTREAM_ADDR"); value != "" {
		addr = value
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/inventory/:id", getInventory)
	router.POST("/payments", createPayment)

	slog.Info("fake upstream listening", "addr", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		slog.Error("upstream stopped", "error", err)
		os.Exit(1)
	}
}

func getInventory(c *gin.Context) {
	if !sleepOrCancel(c, randomDuration(50*time.Millisecond, 500*time.Millisecond)) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"product_id": c.Param("id"), "stock": rand.IntN(100)})
}

func createPayment(c *gin.Context) {
	if !sleepOrCancel(c, randomDuration(20*time.Millisecond, 80*time.Millisecond)) {
		return
	}
	if rand.Float64() < paymentFailureRate {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "acquirer unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "captured"})
}

func sleepOrCancel(c *gin.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func randomDuration(minDuration, maxDuration time.Duration) time.Duration {
	return minDuration + time.Duration(rand.Int64N(int64(maxDuration-minDuration)))
}
