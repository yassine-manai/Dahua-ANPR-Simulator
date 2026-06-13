package models

// ── 5.1 Device Info ──────────────────────────────────────────────────────────

// DeviceInfoPayload matches §5.1 Device Basic Information Message Definition.
type DeviceInfoPayload struct {
	DeviceName   string `json:"DeviceName"`            // Yes
	DeviceModel  string `json:"DeviceModel"`           // Yes
	DeviceType   string `json:"DeviceType"`            // Yes — "Tollgate" | "E-Police"
	Manufacturer string `json:"Manufacturer"`          // Yes
	IPAddress    string `json:"IPAddress,omitempty"`   // No
	IPv6Address  string `json:"IPv6Address,omitempty"` // No
	MACAddress   string `json:"MACAddress,omitempty"`  // No
	DeviceID     string `json:"DeviceID"`              // Yes — UUID
}

type BasicReply struct {
	Result  bool   `json:"Result"`
	Message string `json:"Message"`
}

// ── 5.2 Heartbeat ─────────────────────────────────────────────────────────────

// HeartbeatPayload matches §5.2 Heartbeat push message definition.
// Active must always be the literal string "keepAlive".
type HeartbeatPayload struct {
	Active   string `json:"Active"`   // Yes — always "keepAlive"
	DeviceID string `json:"DeviceID"` // Yes — same UUID as DeviceInfo
}

// HeartbeatReply matches §5.2 Heartbeat reply message definition.
// Active is bool (true = heartbeat acknowledged).
type HeartbeatReply struct {
	Active   bool   `json:"Active"`   // Yes — true: success
	DeviceID string `json:"DeviceID"` // same UUID echoed back
}

// ── 5.3 ANPR / TollgateInfo ──────────────────────────────────────────────────

// ANPRPayload is the full body for POST /NotificationInfo/TollgateInfo.
type ANPRPayload struct {
	Picture ANPRPicture `json:"Picture"`
}

// ANPRPicture holds all image sub-types for a tollgate capture.
type ANPRPicture struct {
	NormalPic  *PicItem      `json:"NormalPic,omitempty"`  // Original scene — No
	CombinPic  []PicItem     `json:"CombinPic,omitempty"`  // Combined drawings array — No
	CutoutPic  *PicItem      `json:"CutoutPic,omitempty"`  // Plate crop — No
	VehiclePic *PicItem      `json:"VehiclePic,omitempty"` // Vehicle crop — No
	FacePic    []ANPRFacePic `json:"FacePic,omitempty"`    // Face crops array — No
	Plate      ANPRPlate     `json:"Plate"`                // Plate info block — Yes
	Vehicle    *ANPRVehicle  `json:"Vehicle,omitempty"`    // Vehicle info — No
	SnapInfo   ANPRSnapInfo  `json:"SnapInfo"`             // Snap info block — Yes
}

// PicItem is a single picture: filename + base64 content.
type PicItem struct {
	PicName string `json:"PicName"`
	Content string `json:"Content"`
}

// ANPRFacePic is one face image with its type classification (Appendix 1.7).
type ANPRFacePic struct {
	PicType int    `json:"PicType"` // 0=Other 1=Driver 2=Co-pilot
	PicName string `json:"PicName"`
	Content string `json:"Content"`
}

// ANPRPlate holds all plate recognition fields per §5.3 Plate block.
type ANPRPlate struct {
	IsExist     bool   `json:"IsExist"`              // Yes
	PlateNumber string `json:"PlateNumber"`          // No — char(64) UTF-8/GB2312/hex
	ADR         string `json:"ADR,omitempty"`        // No — dangerous chemical plate
	PlateColor  string `json:"PlateColor"`           // Yes — Appendix 1.1
	PlateType   string `json:"PlateType"`            // Yes — Appendix 1.2
	Confidence  int    `json:"Confidence,omitempty"` // No — 0-255
	BoundingBox []int  `json:"BoundingBox"`          // Yes — [left,up,right,down]
	UploadNum   int    `json:"UploadNum,omitempty"`  // No — self-incrementing serial
	Channel     int    `json:"Channel,omitempty"`    // No — 0-based channel index
	Region      string `json:"Region,omitempty"`     // No — ISO 3166
}

// ANPRVehicle holds vehicle recognition fields per §5.3 Vehicle block.
type ANPRVehicle struct {
	VehicleColor       string `json:"VehicleColor,omitempty"`       // No — Appendix 1.3
	VehicleSign        string `json:"VehicleSign,omitempty"`        // No — Appendix 1.4 (brand)
	VehicleType        string `json:"VehicleType,omitempty"`        // No — Appendix 1.5
	VehicleSeries      string `json:"VehicleSeries,omitempty"`      // No — model series
	Speed              int    `json:"Speed,omitempty"`              // No — km/h
	VehicleBoundingBox []int  `json:"VehicleBoundingBox,omitempty"` // No — [left,up,right,down]
}

// ANPRSnapInfo holds capture metadata per §5.3 SnapInfo block.
// DeviceID and InCarPeopleNum are also at this level per spec.
type ANPRSnapInfo struct {
	TriggerSource    string `json:"Source,omitempty"`           // No — Unknown/Coil/Radar/Video
	SnapTime         string `json:"SnapTime,omitempty"`         // No — yyyy-mm-dd hh:mm:ss
	AccurateTime     string `json:"AccurateTime,omitempty"`     // No — yyyy-mm-dd hh:mm:ss.sss
	TimeZone         int    `json:"TimeZone"`                   // No — Appendix 1.8
	DSTTune          int    `json:"DSTTune"`                    // No — 0=normal 1=DST
	SnapAddress      string `json:"SnapAddress,omitempty"`      // No — char(64)
	LanNo            int    `json:"LanNo,omitempty"`            // No — starts from 1
	Direction        string `json:"Direction,omitempty"`        // No — Obverse/Reverse
	OpenStrobe       bool   `json:"OpenStrobe"`                 // No — gate control
	AllowUser        bool   `json:"AllowUser"`                  // No — on allowlist
	AllowUserEndTime string `json:"AllowUserEndTime,omitempty"` // No — yyyy-mm-dd hh:mm:ss
	BlockUser        bool   `json:"BlockUser"`                  // No — on blocklist
	BlockUserEndTime string `json:"BlockUserEndTime,omitempty"` // No — yyyy-mm-dd hh:mm:ss
	DefenceCode      string `json:"DefenceCode"`                // Yes — anti-counterfeiting
	DeviceID         string `json:"DeviceID"`                   // Yes — same UUID as DeviceInfo
	InCarPeopleNum   int    `json:"InCarPeopleNum,omitempty"`   // No — people count in vehicle
}

// ANPRReply is the response body for a TollgateInfo push per §5.3 reply definition.
type ANPRReply struct {
	Result      bool   `json:"Result"`                // Yes — true=success false=failed
	DeviceID    string `json:"DeviceID"`              // Yes — UUID
	PlateNumber string `json:"PlateNumber,omitempty"` // No — echo of received plate
	AllowUser   bool   `json:"AllowUser,omitempty"`   // No — allowlist result
	BlockUser   bool   `json:"BlockUser,omitempty"`   // No — blocklist result
	OpenStrobe  bool   `json:"OpenStrobe,omitempty"`  // No — gate open instruction
}

// ── 5.4 Parking / ParkingInfo ────────────────────────────────────────────────

// ParkingPayload is the full body for POST /NotificationInfo/ParkingInfo.
type ParkingPayload struct {
	Picture ParkingPicture `json:"Picture"`
}

// ParkingPicture holds all image sub-types for a parking capture.
// NormalPic supports dual-picture (primary + secondary) per spec.
type ParkingPicture struct {
	NormalPic   *ParkingNormalPic `json:"NormalPic,omitempty"`  // No — scene with optional 2nd frame
	CombinPic   []PicItem         `json:"CombinPic,omitempty"`  // No — combined drawings array
	VehiclePic  *PicItem          `json:"VehiclePic,omitempty"` // No — vehicle crop
	Plate       ParkingPlate      `json:"Plate"`                // Yes — plate info block
	Vehicle     *ParkingVehicle   `json:"Vehicle,omitempty"`    // No — vehicle info
	ParkingInfo ParkingInfo       `json:"ParkingInfo"`          // Parking info block — Yes
}

// ParkingNormalPic is the original scene image, optionally with a second frame.
type ParkingNormalPic struct {
	PicName    string `json:"PicName"`
	Width      int    `json:"Width,omitempty"`
	Height     int    `json:"Height,omitempty"`
	Content    string `json:"Content"`
	PicName2nd string `json:"PicName_2nd,omitempty"` // No — second picture filename
	Width2nd   int    `json:"Width_2nd,omitempty"`   // No — second picture width
	Height2nd  int    `json:"Height_2nd,omitempty"`  // No — second picture height
	Content2nd string `json:"Content_2nd,omitempty"` // No — second picture base64
}

// ParkingPlate holds plate recognition fields per §5.4 Plate block.
type ParkingPlate struct {
	IsExist     bool   `json:"IsExist"`              // Yes
	PlateNumber string `json:"PlateNumber"`          // No — char(64) UTF-8/GB2312/hex
	PlateColor  string `json:"PlateColor"`           // Yes — Appendix 1.1
	PlateType   string `json:"PlateType"`            // Yes — Appendix 1.2
	Confidence  int    `json:"Confidence,omitempty"` // No — 0-100
	BoundingBox []int  `json:"BoundingBox"`          // Yes — [left,up,right,down]
	Region      string `json:"Region,omitempty"`     // No — ISO 3166
}

// ParkingVehicle holds vehicle recognition fields per §5.4 Vehicle block.
type ParkingVehicle struct {
	VehicleColor       string `json:"VehicleColor,omitempty"`       // No — Appendix 1.3
	VehicleSign        string `json:"VehicleSign,omitempty"`        // No — Appendix 1.4 (brand)
	VehicleType        string `json:"VehicleType,omitempty"`        // No — Appendix 1.5
	VehicleSeries      string `json:"VehicleSeries,omitempty"`      // No — model series
	VehicleBoundingBox []int  `json:"VehicleBoundingBox,omitempty"` // No — [left,up,right,down]
}

// ParkingRelationship links an exit event to its matching entry record.
type ParkingRelationship struct {
	Confidence      int    `json:"Confidence,omitempty"`  // No — 0-100
	ParkingStallsNo string `json:"ParkingStallsNo"`       // Yes — stall ID
	PlateNumber     string `json:"PlateNumber,omitempty"` // No — char(32)
}

// ParkingInfo holds capture metadata per §5.4 ParkingInfo block.
type ParkingInfo struct {
	SnapTime         string                `json:"SnapTime"`                   // Yes — yyyy-mm-dd hh:mm:ss
	TimeZone         int                   `json:"TimeZone"`                   // No — Appendix 1.8
	DSTTune          int                   `json:"DSTTune"`                    // No — 0=normal 1=DST
	Channel          int                   `json:"Channel"`                    // No — 0-based
	DetectRegionName string                `json:"DetectRegionName,omitempty"` // No — char(64)
	ParkingStallsNo  string                `json:"ParkingStallsNo"`            // Yes — char(64)
	Direction        string                `json:"Direction,omitempty"`        // No — Obverse/Reverse/Unknow
	ParkingStatus    int                   `json:"ParkingStatus"`              // YES — 0-4
	AllowUser        bool                  `json:"AllowUser"`                  // No
	BlockUser        bool                  `json:"BlockUser"`                  // No
	Relationship     []ParkingRelationship `json:"Relationship,omitempty"`     // C — exit only
	DeviceID         string                `json:"DeviceID,omitempty"`         // C — carry when DeviceInfo supported
	InRecordID       string                `json:"inRecordId,omitempty"`       // YES — only on exit to match entry
}

// ParkingReply is the response body for a ParkingInfo push per §5.4 reply definition.
type ParkingReply struct {
	Result  bool   `json:"Result"`  // Yes
	Message string `json:"Message"` // Yes
	RspTime string `json:"RspTime"` // Yes — reply timestamp
}

// ── 5.5 Alarm / AlarmInfo ────────────────────────────────────────────────────

// AlarmPayload is the full body for POST /NotificationInfo/AlarmInfo.
type AlarmPayload struct {
	AlarmInfo AlarmInfo `json:"AlarmInfo"`
	DeviceID  string    `json:"DeviceID,omitempty"` // C — carry when DeviceInfo supported
}

// AlarmInfo holds the alarm event fields per §5.5 definition.
type AlarmInfo struct {
	Time            string      `json:"Time"`                      // Yes — yyyy-mm-dd hh:mm:ss
	Type            int         `json:"Type"`                      // Yes — Appendix 1.6
	State           string      `json:"State,omitempty"`           // No — Pluse/Start/Stop (v1.07+)
	Confidence      int         `json:"Confidence,omitempty"`      // No — 0-100
	ParkingStallsNo string      `json:"ParkingStallsNo,omitempty"` // No — char(64)
	PlateInfoList   []PlateInfo `json:"PlateInfoList,omitempty"`   // No — array, multiple plates for type 0
	TimeZone        int         `json:"TimeZone"`                  // No — Appendix 1.8
	DSTTune         int         `json:"DSTTune"`                   // No — 0=normal 1=DST
}

// PlateInfo is one entry in AlarmInfo.PlateInfoList.
type PlateInfo struct {
	PlateNumber string `json:"PlateNumber,omitempty"` // No — char(64)
	EntryTime   string `json:"EntryTime,omitempty"`   // C — required for overtime parking etc.
	TimeZone    int    `json:"TimeZone"`              // No — Appendix 1.8
	DSTTune     int    `json:"DSTTune"`               // No — 0=normal 1=DST
}

// AlarmReply is the response body for an AlarmInfo push per §5.5 reply definition.
type AlarmReply struct {
	Result  bool   `json:"Result"`  // Yes
	Message string `json:"Message"` // Yes
	RspTime string `json:"RspTime"` // Yes — reply timestamp
}

// ── Send override wrappers ────────────────────────────────────────────────────
// These are what the UI POSTs to the local sim API.
// Each has an optional Overrides map that deep-merges into the default payload.

type SendRequest struct {
	Overrides map[string]interface{} `json:"overrides,omitempty"`
}
