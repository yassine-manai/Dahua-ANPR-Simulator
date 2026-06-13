package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	stopHeartbeat   chan struct{}
	log             []LogEntry
	logMu           sync.Mutex
	lastSentPayload map[string]interface{}
}

type Handler struct {
	cfg                *config.SimConfig
	mu                 sync.Mutex
	runtime            map[string]*runtimeCamera
	pendingManualSnaps map[string]int // camera ID → channel
	pendingMu          sync.Mutex
}

func New(cfg *config.SimConfig) *Handler {
	h := &Handler{
		cfg:                cfg,
		runtime:            make(map[string]*runtimeCamera),
		pendingManualSnaps: make(map[string]int),
	}
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
	if cam.DeviceID != "" && h.cfg.ExistsByDeviceID(cam.DeviceID) {
		c.JSON(http.StatusConflict, gin.H{"error": "device_id already exists"})
		return
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
	existing, ok := h.cfg.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}
	var cam config.CameraConfig
	if err := c.ShouldBindJSON(&cam); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cam.ID = id
	if cam.DeviceID != "" && cam.DeviceID != existing.DeviceID && h.cfg.ExistsByDeviceID(cam.DeviceID) {
		c.JSON(http.StatusConflict, gin.H{"error": "device_id already exists"})
		return
	}
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
				PlateType:   "",
				Confidence:  0,
				BoundingBox: []int{0, 0, 0, 0},
				Channel:     0,
			},
			Vehicle: &models.ANPRVehicle{},
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
				PlateType:   "",
				Confidence:  0,
				BoundingBox: []int{0, 0, 0, 0},
			},
			Vehicle: &models.ParkingVehicle{},
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
				DeviceID:        cam.DeviceID,
			},
		},
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

// ── ManSnap / LPN Confirmation ────────────────────────────────────────────────

type PendingConfirmView struct {
	CameraID     string                 `json:"camera_id"`
	CameraLabel  string                 `json:"camera_label"`
	CameraIP     string                 `json:"camera_ip"`
	Channel      int                    `json:"channel"`
	LastPayload  map[string]interface{} `json:"last_payload"`
	DeviceID     string                 `json:"device_id"`
	ResponseType string                 `json:"response_type"` // "with_last_payload" or "manual_lpn"
	LastPlate    string                 `json:"last_plate"`
}

func (h *Handler) ManualSnap(c *gin.Context) {
	action := c.Query("action")
	if action != "" && action != "manSnap" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported action"})
		return
	}

	camID := c.Query("cam_id")
	if camID == "" {
		camID = c.Query("device_id")
	}
	if camID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing cam_id or device_id"})
		return
	}

	channelStr := c.DefaultQuery("channel", "0")
	channel, err := strconv.Atoi(channelStr)
	if err != nil || channel < 0 || channel > 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel, must be 0-3"})
		return
	}
	channel = channel + 1

	var cam config.CameraConfig
	var found bool
	for _, cc := range h.cfg.All() {
		if cc.DeviceID == camID {
			cam = cc
			found = true
			break
		}
	}
	if !found {
		if cc, ok := h.cfg.Get(camID); ok {
			cam = cc
			found = true
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}

	rt := h.rt(cam.ID)

	// get last payload
	rt.logMu.Lock()
	lastPayload := rt.lastSentPayload
	rt.logMu.Unlock()

	if lastPayload == nil {
		now := time.Now()
		lastPayload = map[string]interface{}{
			"Picture": map[string]interface{}{
				"Plate": map[string]interface{}{
					"IsExist":     false,
					"PlateNumber": "",
					"PlateColor":  "White",
					"PlateType":   "",
					"Confidence":  0,
					"BoundingBox": []int{0, 0, 0, 0},
				},
				"Vehicle": map[string]interface{}{
					"VehicleColor": "White",
					"VehicleType":  "PassengerCar",
				},
				"SnapInfo": map[string]interface{}{
					"Source":       "Video",
					"SnapTime":     now.Format("2006-01-02 15:04:05"),
					"AccurateTime": now.Format("2006-01-02 15:04:05.000"),
					"TimeZone":     2,
					"DSTTune":      0,
					"LanNo":        1,
					"Direction":    "Obverse",
					"OpenStrobe":   false,
					"AllowUser":    false,
					"BlockUser":    false,
					"DefenceCode":  "SIM001",
					"DeviceID":     cam.DeviceID,
				},
			},
		}
	}

	h.pendingMu.Lock()
	h.pendingManualSnaps[cam.ID] = channel
	h.pendingMu.Unlock()

	c.JSON(http.StatusOK, gin.H{})
}

func (h *Handler) GetPendingConfirmations(c *gin.Context) {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()

	out := []PendingConfirmView{}
	for camID, channel := range h.pendingManualSnaps {
		cam, ok := h.cfg.Get(camID)
		if !ok {
			continue
		}
		rt := h.rt(camID)
		rt.logMu.Lock()
		lp := rt.lastSentPayload
		rt.logMu.Unlock()

		responseType := "with_last_payload"
		if lp == nil {
			responseType = "manual_lpn"
			lp = map[string]interface{}{
				"Picture": map[string]interface{}{
					"Plate": map[string]interface{}{
						"IsExist":     false,
						"PlateNumber": "",
						"PlateColor":  "White",
						"PlateType":   "",
						"Confidence":  0,
						"BoundingBox": []int{0, 0, 0, 0},
					},
					"Vehicle": map[string]interface{}{
						"VehicleColor": "White",
						"VehicleType":  "PassengerCar",
					},
					"SnapInfo": map[string]interface{}{
						"Source":      "Video",
						"TimeZone":    2,
						"DSTTune":     0,
						"LanNo":       1,
						"Direction":   "Obverse",
						"OpenStrobe":  false,
						"AllowUser":   false,
						"BlockUser":   false,
						"DefenceCode": "SIM001",
						"DeviceID":    cam.DeviceID,
					},
				},
			}
		}

		lastPlate := ""
		if lp != nil {
			if pic, ok := lp["Picture"].(map[string]interface{}); ok {
				if plate, ok := pic["Plate"].(map[string]interface{}); ok {
					if pn, ok := plate["PlateNumber"].(string); ok {
						lastPlate = pn
					}
				}
			}
		}

		out = append(out, PendingConfirmView{
			CameraID:     camID,
			CameraLabel:  cam.Label,
			CameraIP:     cam.IPAddress,
			Channel:      channel,
			LastPayload:  lp,
			DeviceID:     cam.DeviceID,
			ResponseType: responseType,
			LastPlate:    lastPlate,
		})
	}
	c.JSON(http.StatusOK, out)
}

type ConfirmLPNRequest struct {
	CameraID    string `json:"camera_id"`
	PlateNumber string `json:"plate_number"`
	Mode        string `json:"mode"` // "reuse_last" (default) or "fake"
}

func (h *Handler) ConfirmLPN(c *gin.Context) {
	var req ConfirmLPNRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.CameraID == "" || req.PlateNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera_id and plate_number are required"})
		return
	}

	cam, ok := h.cfg.Get(req.CameraID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "camera not found"})
		return
	}

	h.pendingMu.Lock()
	ch, exists := h.pendingManualSnaps[req.CameraID]
	rt := h.rt(cam.ID)
	if !exists {
		h.pendingMu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "no pending manual snap for this camera"})
		return
	}
	delete(h.pendingManualSnaps, req.CameraID)
	h.pendingMu.Unlock()

	now := time.Now()

	// build fresh default payload regardless of mode
	payload := map[string]interface{}{
		"Picture": map[string]interface{}{
			"Plate": map[string]interface{}{
				"IsExist":     true,
				"PlateNumber": req.PlateNumber,
				"PlateColor":  "White",
				"PlateType":   "",
				"Confidence":  90,
				"BoundingBox": []int{100, 200, 300, 260},
				"Channel":     ch - 1,
			},
			"Vehicle": map[string]interface{}{
				"VehicleColor":       "White",
				"VehicleType":        "PassengerCar",
				"VehicleBoundingBox": []int{0, 0, 0, 0},
			},
			"SnapInfo": map[string]interface{}{
				"Source":       "Realtime",
				"SnapTime":     now.Format("2006-01-02 15:04:05"),
				"AccurateTime": now.Format("2006-01-02 15:04:05.000"),
				"TimeZone":     2,
				"DSTTune":      0,
				"LanNo":        1,
				"Direction":    "Obverse",
				"OpenStrobe":   false,
				"AllowUser":    false,
				"BlockUser":    false,
				"DefenceCode":  "SIM001",
				"DeviceID":     cam.DeviceID,
			},
		},
	}

	// "reuse_last" — start from last payload to preserve images, then override plate
	if req.Mode == "reuse_last" || req.Mode == "" {
		rt.logMu.Lock()
		lp := rt.lastSentPayload
		rt.logMu.Unlock()

		if lp != nil {
			data, _ := json.Marshal(lp)
			json.Unmarshal(data, &payload)
		}
		pic, _ := payload["Picture"].(map[string]interface{})
		if pic == nil {
			pic = map[string]interface{}{}
			payload["Picture"] = pic
		}
		pic["Plate"] = map[string]interface{}{
			"IsExist":     true,
			"PlateNumber": req.PlateNumber,
			"PlateColor":  "White",
			"PlateType":   "",
			"Confidence":  90,
			"BoundingBox": []int{100, 200, 300, 260},
			"Channel":     ch - 1,
		}
		if _, ok := pic["Vehicle"]; !ok {
			pic["Vehicle"] = map[string]interface{}{
				"VehicleColor":       "White",
				"VehicleType":        "PassengerCar",
				"VehicleBoundingBox": []int{0, 0, 0, 0},
			}
		}
		pic["SnapInfo"] = map[string]interface{}{
			"Source":       "Realtime",
			"SnapTime":     now.Format("2006-01-02 15:04:05"),
			"AccurateTime": now.Format("2006-01-02 15:04:05.000"),
			"TimeZone":     2,
			"DSTTune":      0,
			"LanNo":        1,
			"Direction":    "Obverse",
			"OpenStrobe":   false,
			"AllowUser":    false,
			"BlockUser":    false,
			"DefenceCode":  "SIM001",
			"DeviceID":     cam.DeviceID,
		}
	}

	// POST to backend as TollgateInfo
	status, res, err := h.post(cam, "/NotificationInfo/TollgateInfo", payload)

	// store as last sent payload
	rt.logMu.Lock()
	rt.lastSentPayload = payload
	rt.logMu.Unlock()

	// log the outbound request
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	reqBody, _ := json.Marshal(payload)
	resBody := ""
	if err == nil {
		resBody = string(res)
	}
	h.appendLog(cam.ID, LogEntry{
		Time:       now.Format("2006-01-02 15:04:05"),
		Endpoint:   "/NotificationInfo/TollgateInfo",
		Method:     "POST",
		ReqBody:    string(reqBody),
		ResBody:    resBody,
		StatusCode: status,
		Error:      errStr,
	})

	c.JSON(http.StatusOK, gin.H{
		"confirmed": true,
		"camera_id": req.CameraID,
		"status":    status,
	})
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

	// store last sent payload for ManSnap confirmation
	if err == nil && payload != nil {
		if m, ok := payload.(map[string]interface{}); ok {
			rt := h.rt(cameraID)
			rt.logMu.Lock()
			rt.lastSentPayload = m
			rt.logMu.Unlock()
		}
	}

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
