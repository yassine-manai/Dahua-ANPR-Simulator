package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simulator/config"
	"github.com/simulator/models"
)

// ── Log ───────────────────────────────────────────────────────────────────────

const maxLogEntries = 200

type LogEntry struct {
	Time       string `json:"time"`
	Endpoint   string `json:"endpoint"`
	Method     string `json:"method"`
	ReqBody    string `json:"req_body"`
	ResBody    string `json:"res_body"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
}

// ── Runtime state ─────────────────────────────────────────────────────────────

type runtimeCamera struct {
	stopHeartbeat chan struct{}
	log           []LogEntry
	logMu         sync.Mutex
}

// Handler holds all runtime state and the persisted config.
type Handler struct {
	cfg     *config.SimConfig
	mu      sync.Mutex
	runtime map[string]*runtimeCamera // keyed by camera ID
}

func New(cfg *config.SimConfig) *Handler {
	h := &Handler{
		cfg:     cfg,
		runtime: make(map[string]*runtimeCamera),
	}
	// Init runtime entries for cameras loaded from disk.
	for _, cam := range cfg.All() {
		h.runtime[cam.ID] = &runtimeCamera{}
	}
	return h
}

func (h *Handler) runtime_(id string) *runtimeCamera {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.runtime[id]; !ok {
		h.runtime[id] = &runtimeCamera{}
	}
	return h.runtime[id]
}

func (h *Handler) appendLog(id string, entry LogEntry) {
	rt := h.runtime_(id)
	rt.logMu.Lock()
	defer rt.logMu.Unlock()
	rt.log = append(rt.log, entry)
	if len(rt.log) > maxLogEntries {
		rt.log = rt.log[len(rt.log)-maxLogEntries:]
	}
}

// ── Camera CRUD ───────────────────────────────────────────────────────────────

// GET /sim/cameras
func (h *Handler) ListCameras(c *gin.Context) {
	c.JSON(http.StatusOK, h.cfg.All())
}

// POST /sim/cameras
func (h *Handler) AddCamera(c *gin.Context) {
	var cam config.CameraConfig
	if err := c.ShouldBindJSON(&cam); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if cam.ID == "" {
		cam.ID = fmt.Sprintf("cam_%d", time.Now().UnixNano())
	}
	h.cfg.Upsert(cam)
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cam)
}

// PUT /sim/cameras/:id
func (h *Handler) UpdateCamera(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.cfg.Get(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	var cam config.CameraConfig
	if err := c.ShouldBindJSON(&cam); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cam.ID = id
	h.cfg.Upsert(cam)
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cam)
}

// DELETE /sim/cameras/:id
func (h *Handler) DeleteCamera(c *gin.Context) {
	id := c.Param("id")
	h.stopHeartbeatIfRunning(id)
	h.cfg.Delete(id)
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// ── Log ───────────────────────────────────────────────────────────────────────

// GET /sim/cameras/:id/log
func (h *Handler) GetLog(c *gin.Context) {
	id := c.Param("id")
	rt := h.runtime_(id)
	rt.logMu.Lock()
	defer rt.logMu.Unlock()
	// Return a copy in reverse order (newest first)
	out := make([]LogEntry, len(rt.log))
	for i, e := range rt.log {
		out[len(rt.log)-1-i] = e
	}
	c.JSON(http.StatusOK, out)
}

// DELETE /sim/cameras/:id/log
func (h *Handler) ClearLog(c *gin.Context) {
	id := c.Param("id")
	rt := h.runtime_(id)
	rt.logMu.Lock()
	rt.log = nil
	rt.logMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"cleared": true})
}

// ── Heartbeat control ─────────────────────────────────────────────────────────

// POST /sim/cameras/:id/heartbeat/start
func (h *Handler) StartHeartbeat(c *gin.Context) {
	id := c.Param("id")
	cam, ok := h.cfg.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	interval := cam.HeartbeatInterval
	if interval <= 0 {
		interval = 30
	}
	h.stopHeartbeatIfRunning(id)
	stop := make(chan struct{})
	h.mu.Lock()
	if h.runtime[id] == nil {
		h.runtime[id] = &runtimeCamera{}
	}
	h.runtime[id].stopHeartbeat = stop
	h.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()
		// Send immediately on start
		h.doSendHeartbeat(cam)
		for {
			select {
			case <-ticker.C:
				h.doSendHeartbeat(cam)
			case <-stop:
				return
			}
		}
	}()
	c.JSON(http.StatusOK, gin.H{"started": true, "interval": interval})
}

// POST /sim/cameras/:id/heartbeat/stop
func (h *Handler) StopHeartbeat(c *gin.Context) {
	id := c.Param("id")
	h.stopHeartbeatIfRunning(id)
	c.JSON(http.StatusOK, gin.H{"stopped": true})
}

func (h *Handler) stopHeartbeatIfRunning(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if rt, ok := h.runtime[id]; ok && rt.stopHeartbeat != nil {
		close(rt.stopHeartbeat)
		rt.stopHeartbeat = nil
	}
}

// ── Send endpoints ────────────────────────────────────────────────────────────

// POST /sim/cameras/:id/send/deviceinfo
func (h *Handler) SendDeviceInfo(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	payload := models.DeviceInfoPayload{
		DeviceName:   cam.DeviceName,
		DeviceModel:  cam.DeviceModel,
		DeviceType:   cam.DeviceType,
		Manufacturer: cam.Manufacturer,
		IPAddress:    cam.IPAddress,
		DeviceID:     cam.DeviceID,
	}
	merged := h.mergeOverrides(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/DeviceInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/DeviceInfo", merged, status, res, err)
}

// POST /sim/cameras/:id/send/keepalive
func (h *Handler) SendKeepAlive(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	h.doSendHeartbeat(cam)
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

// POST /sim/cameras/:id/send/anpr
func (h *Handler) SendANPR(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	now := time.Now()
	payload := models.ANPRPayload{
		Picture: models.ANPRPicture{
			Plate: models.ANPRPlate{
				IsExist:     true,
				PlateNumber: "",
				PlateColor:  "White",
				PlateType:   "Normal",
				Confidence:  90,
				BoundingBox: []int{100, 200, 300, 260},
				Channel:     0,
			},
		},
		SnapInfo: models.ANPRSnapInfo{
			TriggerSource: "Video",
			SnapTime:      now.Format("2006-01-02 15:04:05"),
			AccurateTime:  now.Format("2006-01-02 15:04:05.000"),
			TimeZone:      2, // GMT+02:00 Tunisia
			DSTTune:       0,
			LanNo:         1,
			Direction:     "Obverse",
			OpenStrobe:    false,
			AllowUser:     false,
			BlockUser:     false,
			DefenceCode:   "SIM001",
			DeviceID:      cam.DeviceID,
		},
	}
	merged := h.mergeOverrides(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/TollgateInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/TollgateInfo", merged, status, res, err)
}

// POST /sim/cameras/:id/send/parking
func (h *Handler) SendParking(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	now := time.Now()
	payload := models.ParkingPayload{
		Picture: models.ParkingPicture{
			Plate: models.ParkingPlate{
				IsExist:     true,
				PlateNumber: "",
				PlateColor:  "White",
				PlateType:   "Normal",
				Confidence:  85,
				BoundingBox: []int{100, 200, 300, 260},
			},
		},
		ParkingInfo: models.ParkingInfo{
			SnapTime:        now.Format("2006-01-02 15:04:05"),
			TimeZone:        2,
			DSTTune:         0,
			Channel:         0,
			ParkingStallsNo: "A01",
			Direction:       "Obverse",
			ParkingStatus:   0,
			AllowUser:       false,
			BlockUser:       false,
		},
		DeviceID: cam.DeviceID,
	}
	merged := h.mergeOverrides(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/ParkingInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/ParkingInfo", merged, status, res, err)
}

// POST /sim/cameras/:id/send/alarm
func (h *Handler) SendAlarm(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	now := time.Now()
	payload := models.AlarmPayload{
		AlarmInfo: models.AlarmInfo{
			Time:     now.Format("2006-01-02 15:04:05"),
			Type:     0,
			State:    "Pluse",
			TimeZone: 2,
			DSTTune:  0,
		},
		DeviceID: cam.DeviceID,
	}
	merged := h.mergeOverrides(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/AlarmInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/AlarmInfo", merged, status, res, err)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (h *Handler) resolveCamera(c *gin.Context) (config.CameraConfig, bool) {
	id := c.Param("id")
	cam, ok := h.cfg.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return config.CameraConfig{}, false
	}
	return cam, true
}

// mergeOverrides unmarshals the request body overrides map and deep-merges
// them into the default payload by round-tripping through JSON.
func (h *Handler) mergeOverrides(c *gin.Context, defaultPayload interface{}) map[string]interface{} {
	// Marshal default
	base, _ := json.Marshal(defaultPayload)
	var m map[string]interface{}
	json.Unmarshal(base, &m)

	// Read body
	body, _ := io.ReadAll(c.Request.Body)
	if len(body) > 0 {
		var req models.SendRequest
		if err := json.Unmarshal(body, &req); err == nil && req.Overrides != nil {
			for k, v := range req.Overrides {
				m[k] = v
			}
		}
	}
	return m
}

// post sends a JSON POST to the camera's backend URL + path.
func (h *Handler) post(cam config.CameraConfig, path string, payload interface{}) (int, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post marshal: %w", err)
	}

	url := cam.BackendURL + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Connection", "close")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post do: %w", err)
	}
	defer resp.Body.Close()
	resBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(resBody), nil
}

func (h *Handler) doSendHeartbeat(cam config.CameraConfig) {
	payload := models.HeartbeatPayload{
		Active:   "keepAlive",
		DeviceID: cam.DeviceID,
	}
	status, res, err := h.post(cam, "/NotificationInfo/KeepAlive", payload)
	entry := LogEntry{
		Time:       time.Now().Format("15:04:05"),
		Endpoint:   "/NotificationInfo/KeepAlive",
		Method:     "POST",
		StatusCode: status,
		ResBody:    res,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	b, _ := json.Marshal(payload)
	entry.ReqBody = string(b)
	h.appendLog(cam.ID, entry)
}

func (h *Handler) logAndRespond(
	c *gin.Context,
	cameraID string,
	endpoint string,
	payload interface{},
	statusCode int,
	resBody string,
	err error,
) {
	entry := LogEntry{
		Time:       time.Now().Format("15:04:05"),
		Endpoint:   endpoint,
		Method:     "POST",
		StatusCode: statusCode,
		ResBody:    resBody,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	b, _ := json.Marshal(payload)
	entry.ReqBody = string(b)
	h.appendLog(cameraID, entry)

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":    err.Error(),
			"endpoint": endpoint,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   statusCode,
		"response": resBody,
		"endpoint": endpoint,
	})
}
