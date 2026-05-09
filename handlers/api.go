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

type runtimeCamera struct {
	stopHeartbeat chan struct{}
	log           []LogEntry
	logMu         sync.Mutex
}

type Handler struct {
	cfg     *config.SimConfig
	mu      sync.Mutex
	runtime map[string]*runtimeCamera
}

func New(cfg *config.SimConfig) *Handler {
	h := &Handler{cfg: cfg, runtime: make(map[string]*runtimeCamera)}
	for _, cam := range cfg.All() {
		h.runtime[cam.ID] = &runtimeCamera{}
	}
	return h
}

// SetID is kept for backward compatibility but unused with gin (params come from context).
func (h *Handler) SetID(_ string) {}

func (h *Handler) rt(id string) *runtimeCamera {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.runtime[id]; !ok {
		h.runtime[id] = &runtimeCamera{}
	}
	return h.runtime[id]
}

func (h *Handler) appendLog(id string, e LogEntry) {
	rt := h.rt(id)
	rt.logMu.Lock()
	defer rt.logMu.Unlock()
	rt.log = append(rt.log, e)
	if len(rt.log) > maxLogEntries {
		rt.log = rt.log[len(rt.log)-maxLogEntries:]
	}
}

// ── Camera CRUD ───────────────────────────────────────────────────────────────

func (h *Handler) ListCameras(c *gin.Context) {
	c.JSON(http.StatusOK, h.cfg.All())
}

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

func (h *Handler) DeleteCamera(c *gin.Context) {
	id := c.Param("id")
	h.stopHB(id)
	h.cfg.Delete(id)
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// ── Log ───────────────────────────────────────────────────────────────────────

func (h *Handler) GetLog(c *gin.Context) {
	id := c.Param("id")
	rt := h.rt(id)
	rt.logMu.Lock()
	defer rt.logMu.Unlock()
	out := make([]LogEntry, len(rt.log))
	for i, e := range rt.log {
		out[len(rt.log)-1-i] = e
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) ClearLog(c *gin.Context) {
	id := c.Param("id")
	rt := h.rt(id)
	rt.logMu.Lock()
	rt.log = nil
	rt.logMu.Unlock()
	c.JSON(http.StatusOK, gin.H{"cleared": true})
}

// ── Heartbeat ─────────────────────────────────────────────────────────────────

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
	h.stopHB(id)
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
		h.doHeartbeat(cam)
		for {
			select {
			case <-ticker.C:
				h.doHeartbeat(cam)
			case <-stop:
				return
			}
		}
	}()
	c.JSON(http.StatusOK, gin.H{"started": true, "interval": interval})
}

func (h *Handler) StopHeartbeat(c *gin.Context) {
	h.stopHB(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"stopped": true})
}

func (h *Handler) stopHB(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if rt, ok := h.runtime[id]; ok && rt.stopHeartbeat != nil {
		close(rt.stopHeartbeat)
		rt.stopHeartbeat = nil
	}
}

// ── Send handlers ─────────────────────────────────────────────────────────────

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
		IPv6Address:  cam.IPv6Address,
		MACAddress:   cam.MACAddress,
		DeviceID:     cam.DeviceID,
	}
	merged := h.merge(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/DeviceInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/DeviceInfo", merged, status, res, err)
}

func (h *Handler) SendKeepAlive(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	h.doHeartbeat(cam)
	c.JSON(http.StatusOK, gin.H{"sent": true})
}

func (h *Handler) SendANPR(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	now := time.Now()
	payload := models.ANPRPayload{
		Picture: models.ANPRPicture{
			// NormalPic / CombinPic / CutoutPic / VehiclePic / FacePic
			// injected via UI overrides when images are selected
			Plate: models.ANPRPlate{
				IsExist:     false,
				PlateNumber: "",
				PlateColor:  "White",
				PlateType:   "Normal",
				Confidence:  0,
				BoundingBox: []int{0, 0, 0, 0},
				Channel:     0,
			},
			Vehicle: &models.ANPRVehicle{},
		},
		SnapInfo: models.ANPRSnapInfo{
			TriggerSource: "Video",
			SnapTime:      now.Format("2006-01-02 15:04:05"),
			AccurateTime:  now.Format("2006-01-02 15:04:05.000"),
			TimeZone:      2,
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
	merged := h.merge(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/TollgateInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/TollgateInfo", merged, status, res, err)
}

func (h *Handler) SendParking(c *gin.Context) {
	cam, ok := h.resolveCamera(c)
	if !ok {
		return
	}
	now := time.Now()
	payload := models.ParkingPayload{
		Picture: models.ParkingPicture{
			// NormalPic / CombinPic / VehiclePic injected via UI overrides
			Plate: models.ParkingPlate{
				IsExist:     false,
				PlateNumber: "",
				PlateColor:  "White",
				PlateType:   "Normal",
				Confidence:  0,
				BoundingBox: []int{0, 0, 0, 0},
			},
			Vehicle: &models.ParkingVehicle{},
		},
		ParkingInfo: models.ParkingInfo{
			SnapTime:        now.Format("2006-01-02 15:04:05"),
			TimeZone:        2,
			DSTTune:         0,
			Channel:         0,
			ParkingStallsNo: "",
			Direction:       "Obverse",
			ParkingStatus:   0,
			AllowUser:       false,
			BlockUser:       false,
		},
		DeviceID: cam.DeviceID,
	}
	merged := h.merge(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/ParkingInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/ParkingInfo", merged, status, res, err)
}

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
	merged := h.merge(c, payload)
	status, res, err := h.post(cam, "/NotificationInfo/AlarmInfo", merged)
	h.logAndRespond(c, cam.ID, "/NotificationInfo/AlarmInfo", merged, status, res, err)
}

// ── Image library ─────────────────────────────────────────────────────────────

func (h *Handler) ListImages(c *gin.Context) {
	c.JSON(http.StatusOK, h.cfg.AllImagesMeta())
}

func (h *Handler) GetImage(c *gin.Context) {
	img, ok := h.cfg.GetImage(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		return
	}
	c.JSON(http.StatusOK, img)
}

func (h *Handler) AddImages(c *gin.Context) {
	var incoming []struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := c.ShouldBindJSON(&incoming); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	added := []config.ImageEntry{}
	skipped := 0
	for _, item := range incoming {
		entry := config.ImageEntry{
			ID:   fmt.Sprintf("img_%d", time.Now().UnixNano()),
			Name: item.Name,
			Data: item.Data,
		}
		if !h.cfg.AddImage(entry) {
			skipped++
			continue
		}
		added = append(added, entry)
		time.Sleep(time.Millisecond)
	}
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"added":   len(added),
		"skipped": skipped,
		"images":  added,
	})
}

func (h *Handler) DeleteImage(c *gin.Context) {
	h.cfg.DeleteImage(c.Param("id"))
	if err := h.cfg.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
}

// ── internals ─────────────────────────────────────────────────────────────────

func (h *Handler) resolveCamera(c *gin.Context) (config.CameraConfig, bool) {
	cam, ok := h.cfg.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return config.CameraConfig{}, false
	}
	return cam, true
}

func (h *Handler) merge(c *gin.Context, def interface{}) map[string]interface{} {
	base, _ := json.Marshal(def)
	var m map[string]interface{}
	json.Unmarshal(base, &m)

	body, _ := io.ReadAll(c.Request.Body)
	defer c.Request.Body.Close()
	if len(body) > 0 {
		var req models.SendRequest
		if err := json.Unmarshal(body, &req); err == nil && req.Overrides != nil {
			deepMerge(m, req.Overrides)
		}
	}
	return m
}

func deepMerge(dst, src map[string]interface{}) {
	for k, v := range src {
		srcMap, srcIsMap := v.(map[string]interface{})
		dstMap, dstIsMap := dst[k].(map[string]interface{})
		if srcIsMap && dstIsMap {
			// Both are maps — recurse
			deepMerge(dstMap, srcMap)
		} else if srcIsMap && dst[k] == nil {
			// dst key missing entirely — copy src map as-is
			dst[k] = v
		} else {
			// Scalar or type mismatch — src wins
			dst[k] = v
		}
	}
}

func (h *Handler) post(cam config.CameraConfig, path string, payload interface{}) (int, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, cam.BackendURL+path, bytes.NewReader(data))
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Connection", "close")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("handlers.post: %w", err)
	}
	defer resp.Body.Close()
	resBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(resBody), nil
}

func (h *Handler) doHeartbeat(cam config.CameraConfig) {
	payload := models.HeartbeatPayload{
		Active:   "keepAlive",
		DeviceID: cam.DeviceID,
	}
	status, res, err := h.post(cam, "/NotificationInfo/KeepAlive", payload)

	reqBytes, _ := json.Marshal(payload)
	e := LogEntry{
		Time:       time.Now().Format("15:04:05"),
		Endpoint:   "/NotificationInfo/KeepAlive",
		Method:     "POST",
		StatusCode: status,
		ReqBody:    string(reqBytes),
		ResBody:    res,
	}
	if err != nil {
		e.Error = err.Error()
	} else {
		// Parse reply to validate backend acknowledged correctly
		var reply models.HeartbeatReply
		if jsonErr := json.Unmarshal([]byte(res), &reply); jsonErr == nil {
			if !reply.Active {
				e.Error = "backend returned Active=false"
			}
		}
	}
	h.appendLog(cam.ID, e)
}

func (h *Handler) logAndRespond(
	c *gin.Context,
	cameraID, endpoint string,
	payload interface{},
	statusCode int, resBody string, err error,
) {
	e := LogEntry{
		Time:       time.Now().Format("15:04:05"),
		Endpoint:   endpoint,
		Method:     "POST",
		StatusCode: statusCode,
		ResBody:    resBody,
	}
	if err != nil {
		e.Error = err.Error()
	}
	b, _ := json.Marshal(payload)
	e.ReqBody = string(b)
	h.appendLog(cameraID, e)

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
