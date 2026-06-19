package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/simulator/config"
	"github.com/simulator/handlers"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	h := handlers.New(cfg)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Static UI
	r.Static("/ui", "./ui")
	r.GET("/", func(c *gin.Context) {
		c.Redirect(301, "/ui/index.html")
	})

	// ── Cameras ──────────────────────────────────────────────────────────────
	sim := r.Group("/sim")

	sim.GET("/cameras", h.ListCameras)
	sim.POST("/cameras", h.AddCamera)
	sim.PUT("/cameras/:id", h.UpdateCamera)
	sim.DELETE("/cameras/:id", h.DeleteCamera)

	sim.GET("/cameras/:id/log", h.GetLog)
	sim.DELETE("/cameras/:id/log", h.ClearLog)

	sim.POST("/cameras/:id/heartbeat/start", h.StartHeartbeat)
	sim.POST("/cameras/:id/heartbeat/stop", h.StopHeartbeat)

	sim.POST("/cameras/:id/send/deviceinfo", h.SendDeviceInfo)
	sim.POST("/cameras/:id/send/keepalive", h.SendKeepAlive)
	sim.POST("/cameras/:id/send/anpr", h.SendANPR)
	sim.POST("/cameras/:id/send/parking", h.SendParking)
	sim.POST("/cameras/:id/send/alarm", h.SendAlarm)

	// ── SSE events for real-time UI ─────────────────────────────────────────────
	sim.GET("/events", h.SSEEvents)

	// ── LPN Confirmation (ManSnap) ──────────────────────────────────────────────
	sim.GET("/cgi-bin/trafficSnap.cgi", h.ManualSnap)
	r.GET("/cgi-bin/trafficSnap.cgi", h.ManualSnap)

	sim.GET("/pending-confirmations", h.GetPendingConfirmations)
	sim.POST("/confirm-lpn", h.ConfirmLPN)

	// ── Image library ─────────────────────────────────────────────────────────
	sim.GET("/images", h.ListImages)
	sim.POST("/images", h.AddImages)
	sim.GET("/images/:id", h.GetImage)
	sim.GET("/images/:id/raw", h.GetImageRaw)
	sim.DELETE("/images/:id", h.DeleteImage)

	addr := ":9797"
	fmt.Printf("\n  Dahua ITS Simulator  →  http://localhost%s\n\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
