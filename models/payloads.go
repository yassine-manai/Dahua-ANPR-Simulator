package models

// ── 5.1 Device Info ──────────────────────────────────────────────────────────

type DeviceInfoPayload struct {
	DeviceName   string `json:"DeviceName"`
	DeviceModel  string `json:"DeviceModel"`
	DeviceType   string `json:"DeviceType"`
	Manufacturer string `json:"Manufacturer"`
	IPAddress    string `json:"IPAddress,omitempty"`
	MACAddress   string `json:"MACAddress,omitempty"`
	DeviceID     string `json:"DeviceID"`
}

type BasicReply struct {
	Result  bool   `json:"Result"`
	Message string `json:"Message"`
}

// ── 5.2 Heartbeat ─────────────────────────────────────────────────────────────

type HeartbeatPayload struct {
	Active   string `json:"Active"`
	DeviceID string `json:"DeviceID"`
}

type HeartbeatReply struct {
	Active   bool   `json:"Active"`
	DeviceID string `json:"DeviceID"`
}

// ── 5.3 ANPR ──────────────────────────────────────────────────────────────────

type ANPRPayload struct {
	Picture  ANPRPicture  `json:"Picture"`
	SnapInfo ANPRSnapInfo `json:"SnapInfo"`
}

type ANPRPicture struct {
	NormalPic  *PicItem     `json:"NormalPic,omitempty"`
	CutoutPic  *PicItem     `json:"CutoutPic,omitempty"`
	VehiclePic *PicItem     `json:"VehiclePic,omitempty"`
	Plate      ANPRPlate    `json:"Plate"`
	Vehicle    *ANPRVehicle `json:"Vehicle,omitempty"`
}

type PicItem struct {
	PicName string `json:"PicName"`
	Content string `json:"Content"`
}

type ANPRPlate struct {
	IsExist     bool   `json:"IsExist"`
	PlateNumber string `json:"PlateNumber,omitempty"`
	PlateColor  string `json:"PlateColor"`
	PlateType   string `json:"PlateType"`
	Confidence  int    `json:"Confidence,omitempty"`
	BoundingBox []int  `json:"BoundingBox"`
	Channel     int    `json:"Channel"`
}

type ANPRVehicle struct {
	VehicleColor string `json:"VehicleColor,omitempty"`
	VehicleSign  string `json:"VehicleSign,omitempty"`
	VehicleType  string `json:"VehicleType,omitempty"`
	Speed        int    `json:"Speed,omitempty"`
}

type ANPRSnapInfo struct {
	TriggerSource string `json:"TriggerSource,omitempty"`
	SnapTime      string `json:"SnapTime"`
	AccurateTime  string `json:"AccurateTime,omitempty"`
	TimeZone      int    `json:"TimeZone"`
	DSTTune       int    `json:"DSTTune"`
	LanNo         int    `json:"LanNo,omitempty"`
	Direction     string `json:"Direction,omitempty"`
	OpenStrobe    bool   `json:"OpenStrobe"`
	AllowUser     bool   `json:"AllowUser"`
	BlockUser     bool   `json:"BlockUser"`
	DefenceCode   string `json:"DefenceCode"`
	DeviceID      string `json:"DeviceID"`
}

type ANPRReply struct {
	Result      bool   `json:"Result"`
	DeviceID    string `json:"DeviceID"`
	PlateNumber string `json:"PlateNumber,omitempty"`
	AllowUser   bool   `json:"AllowUser,omitempty"`
	BlockUser   bool   `json:"BlockUser,omitempty"`
	OpenStrobe  bool   `json:"OpenStrobe,omitempty"`
}

// ── 5.4 Parking ───────────────────────────────────────────────────────────────

type ParkingPayload struct {
	Picture     ParkingPicture `json:"Picture"`
	ParkingInfo ParkingInfo    `json:"ParkingInfo"`
	DeviceID    string         `json:"DeviceID,omitempty"`
}

type ParkingPicture struct {
	NormalPic  *PicItemWithRes `json:"NormalPic,omitempty"`
	VehiclePic *PicItem        `json:"VehiclePic,omitempty"`
	Plate      ParkingPlate    `json:"Plate"`
	Vehicle    *ParkingVehicle `json:"Vehicle,omitempty"`
}

type PicItemWithRes struct {
	PicName string `json:"PicName"`
	Width   int    `json:"Width,omitempty"`
	Height  int    `json:"Height,omitempty"`
	Content string `json:"Content"`
}

type ParkingPlate struct {
	IsExist     bool   `json:"IsExist"`
	PlateNumber string `json:"PlateNumber,omitempty"`
	PlateColor  string `json:"PlateColor"`
	PlateType   string `json:"PlateType"`
	Confidence  int    `json:"Confidence,omitempty"`
	BoundingBox []int  `json:"BoundingBox"`
}

type ParkingVehicle struct {
	VehicleColor string `json:"VehicleColor,omitempty"`
	VehicleSign  string `json:"VehicleSign,omitempty"`
	VehicleType  string `json:"VehicleType,omitempty"`
}

type ParkingInfo struct {
	SnapTime        string `json:"SnapTime"`
	TimeZone        int    `json:"TimeZone"`
	DSTTune         int    `json:"DSTTune"`
	Channel         int    `json:"Channel"`
	ParkingStallsNo string `json:"ParkingStallsNo"`
	Direction       string `json:"Direction,omitempty"`
	ParkingStatus   int    `json:"ParkingStatus"`
	AllowUser       bool   `json:"AllowUser"`
	BlockUser       bool   `json:"BlockUser"`
	InRecordID      string `json:"inRecordId,omitempty"`
}

type ParkingReply struct {
	Result  bool   `json:"Result"`
	Message string `json:"Message"`
	RspTime string `json:"RspTime"`
}

// ── 5.5 Alarm ─────────────────────────────────────────────────────────────────

type AlarmPayload struct {
	AlarmInfo AlarmInfo `json:"AlarmInfo"`
	DeviceID  string    `json:"DeviceID,omitempty"`
}

type AlarmInfo struct {
	Time            string      `json:"Time"`
	Type            int         `json:"Type"`
	State           string      `json:"State,omitempty"`
	Confidence      int         `json:"Confidence,omitempty"`
	ParkingStallsNo string      `json:"ParkingStallsNo,omitempty"`
	PlateInfoList   []PlateInfo `json:"PlateInfoList,omitempty"`
	TimeZone        int         `json:"TimeZone"`
	DSTTune         int         `json:"DSTTune"`
}

type PlateInfo struct {
	PlateNumber string `json:"PlateNumber,omitempty"`
	EntryTime   string `json:"EntryTime,omitempty"`
	TimeZone    int    `json:"TimeZone"`
	DSTTune     int    `json:"DSTTune"`
}

type AlarmReply struct {
	Result  bool   `json:"Result"`
	Message string `json:"Message"`
	RspTime string `json:"RspTime"`
}

// ── Send override wrappers ────────────────────────────────────────────────────
// These are what the UI POSTs to the local sim API.
// Each has an optional Overrides map that deep-merges into the default payload.

type SendRequest struct {
	Overrides map[string]interface{} `json:"overrides,omitempty"`
}
