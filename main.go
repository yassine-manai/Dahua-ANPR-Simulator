package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/simulator/config"
	"github.com/simulator/handlers"
)

func main() {
	// Load persisted config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	h := handlers.New(cfg)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Serve static UI files
	r.Static("/ui", "./ui")
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/ui/index.html")
	})

	// ── Camera CRUD ──
	sim := r.Group("/sim")
	{
		sim.GET("/cameras", h.ListCameras)
		sim.POST("/cameras", h.AddCamera)
		sim.PUT("/cameras/:id", h.UpdateCamera)
		sim.DELETE("/cameras/:id", h.DeleteCamera)

		// Log
		sim.GET("/cameras/:id/log", h.GetLog)
		sim.DELETE("/cameras/:id/log", h.ClearLog)

		// Heartbeat control
		sim.POST("/cameras/:id/heartbeat/start", h.StartHeartbeat)
		sim.POST("/cameras/:id/heartbeat/stop", h.StopHeartbeat)

		// Send actions
		sim.POST("/cameras/:id/send/deviceinfo", h.SendDeviceInfo)
		sim.POST("/cameras/:id/send/keepalive", h.SendKeepAlive)
		sim.POST("/cameras/:id/send/anpr", h.SendANPR)
		sim.POST("/cameras/:id/send/parking", h.SendParking)
		sim.POST("/cameras/:id/send/alarm", h.SendAlarm)
	}

	addr := ":9797"
	fmt.Printf("\n🎥  Dahua ITS Simulator running at http://localhost%s\n\n", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
