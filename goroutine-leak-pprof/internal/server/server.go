package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"goroutine-leak-pprof/internal/client"
	"goroutine-leak-pprof/internal/config"
	"goroutine-leak-pprof/internal/handler"
	"goroutine-leak-pprof/internal/service"
)

const (
	blockProfileRateNs   = 10_000
	mutexProfileFraction = 100
	readHeaderTimeout    = 5 * time.Second
)

type Server struct {
	cfg    config.Config
	logger *slog.Logger
	app    *http.Server
	debug  *http.Server
}

func New(rootCtx context.Context, cfg config.Config, logger *slog.Logger) *Server {
	httpClient := client.NewHTTPClient()
	inventory := client.NewInventoryClient(cfg.UpstreamURL, httpClient)
	payment := client.NewPaymentClient(cfg.UpstreamURL, httpClient, cfg.IsLeaky())

	orders := service.NewOrderService(rootCtx, inventory, payment, cfg.InventoryTimeout, cfg.IsLeaky())
	reports := service.NewReportService(cfg.IsLeaky())

	return &Server{
		cfg:    cfg,
		logger: logger,
		app:    newHTTPServer(cfg.AppAddr, newAppRouter(orders, reports)),
		debug:  newHTTPServer(cfg.DebugAddr, newDebugRouter(cfg.Mode, reports)),
	}
}

func (s *Server) Run(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error { return s.serve("app", s.app) })
	group.Go(func() error { return s.serve("debug", s.debug) })
	group.Go(func() error {
		<-groupCtx.Done()
		return s.shutdown()
	})

	return group.Wait()
}

func (s *Server) serve(name string, srv *http.Server) error {
	s.logger.Info("listening", "server", name, "addr", srv.Addr, "mode", s.cfg.Mode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) shutdown() error {
	s.logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()
	return errors.Join(s.app.Shutdown(ctx), s.debug.Shutdown(ctx))
}

func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout}
}

func newAppRouter(orders *service.OrderService, reports *service.ReportService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	orderHandler := handler.NewOrderHandler(orders)
	reportHandler := handler.NewReportHandler(reports)

	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	v1 := router.Group("/api/v1")
	v1.GET("/orders/:id", orderHandler.GetOrder)
	v1.POST("/orders/checkout", orderHandler.Checkout)
	v1.GET("/reports/sales", reportHandler.SalesReport)

	return router
}

func newDebugRouter(mode config.Mode, reports *service.ReportService) *gin.Engine {
	runtime.SetBlockProfileRate(blockProfileRateNs)
	runtime.SetMutexProfileFraction(mutexProfileFraction)

	router := gin.New()
	router.Use(gin.Recovery())
	pprof.Register(router)

	statsHandler := handler.NewStatsHandler(mode, reports)
	router.GET("/debug/stats", statsHandler.Runtime)
	router.POST("/debug/gc", statsHandler.ForceGC)

	return router
}
