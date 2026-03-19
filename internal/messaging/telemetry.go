package messaging

// Flat telemetry structs matching NovaCore DER type schemas.
// Sign conventions (NovaCore load convention):
//   Battery W: positive=charging, negative=discharging
//   Solar W:   always negative (generation)
//   Meter W:   positive=import, negative=export

// BatteryTelemetry is the telemetry payload for a battery DER.
type BatteryTelemetry struct {
	Type             string  `json:"type"`
	Timestamp        int64   `json:"timestamp"`
	ReadTimeMs       int64   `json:"read_time_ms"`
	Make             string  `json:"make"`
	W                float64 `json:"W"`
	V                float64 `json:"V,omitempty"`
	A                float64 `json:"A,omitempty"`
	SoCNomFract      float64 `json:"SoC_nom_fract"`
	HeatsinkC        float64 `json:"heatsink_C,omitempty"`
	TotalChargeWh    int64   `json:"total_charge_Wh"`
	TotalDischargeWh int64   `json:"total_discharge_Wh"`
	UpperLimitW      float64 `json:"upper_limit_W,omitempty"`
	LowerLimitW      float64 `json:"lower_limit_W,omitempty"`
}

// SolarTelemetry is the telemetry payload for a solar/PV DER.
type SolarTelemetry struct {
	Type              string  `json:"type"`
	Timestamp         int64   `json:"timestamp"`
	ReadTimeMs        int64   `json:"read_time_ms"`
	Make              string  `json:"make"`
	W                 float64 `json:"W"`
	HVLV              float64 `json:"HV_LV,omitempty"`
	A                 float64 `json:"A,omitempty"`
	TotalGenerationWh float64 `json:"total_generation_Wh,omitempty"`
	Mppt1V            float64 `json:"mppt1_V,omitempty"`
	Mppt1A            float64 `json:"mppt1_A,omitempty"`
	Mppt2V            float64 `json:"mppt2_V,omitempty"`
	Mppt2A            float64 `json:"mppt2_A,omitempty"`
	HeatsinkC         float64 `json:"heatsink_C,omitempty"`
	RatedPowerW       float64 `json:"rated_power_W,omitempty"`
	LowerLimitW       float64 `json:"lower_limit_W,omitempty"`
	UpperLimitW       float64 `json:"upper_limit_W,omitempty"`
}

// MeterTelemetry is the telemetry payload for a grid meter DER.
type MeterTelemetry struct {
	Type          string  `json:"type"`
	Timestamp     int64   `json:"timestamp"`
	ReadTimeMs    int64   `json:"read_time_ms"`
	Make          string  `json:"make"`
	W             float64 `json:"W"`
	Hz            float64 `json:"Hz,omitempty"`
	L1W           float64 `json:"L1_W,omitempty"`
	L2W           float64 `json:"L2_W,omitempty"`
	L3W           float64 `json:"L3_W,omitempty"`
	L1V           float64 `json:"L1_V,omitempty"`
	L2V           float64 `json:"L2_V,omitempty"`
	L3V           float64 `json:"L3_V,omitempty"`
	L1A           float64 `json:"L1_A,omitempty"`
	L2A           float64 `json:"L2_A,omitempty"`
	L3A           float64 `json:"L3_A,omitempty"`
	TotalImportWh float64 `json:"total_import_Wh,omitempty"`
	TotalExportWh float64 `json:"total_export_Wh,omitempty"`
}

// V2XChargerTelemetry is the telemetry payload for a bidirectional EV charger.
type V2XChargerTelemetry struct {
	Type                string    `json:"type"`
	Timestamp           int64     `json:"timestamp"`
	ReadTimeMs          int64     `json:"read_time_ms"`
	Make                string    `json:"make"`
	W                   float64   `json:"W"`
	A                   float64   `json:"A,omitempty"`
	V                   float64   `json:"V,omitempty"`
	Hz                  float64   `json:"Hz,omitempty"`
	L1A                 float64   `json:"L1_A,omitempty"`
	L2A                 float64   `json:"L2_A,omitempty"`
	L3A                 float64   `json:"L3_A,omitempty"`
	L1V                 float64   `json:"L1_V,omitempty"`
	L2V                 float64   `json:"L2_V,omitempty"`
	L3V                 float64   `json:"L3_V,omitempty"`
	L1W                 float64   `json:"L1_W,omitempty"`
	L2W                 float64   `json:"L2_W,omitempty"`
	L3W                 float64   `json:"L3_W,omitempty"`
	DCW                 float64   `json:"dc_W,omitempty"`
	DCA                 float64   `json:"dc_A,omitempty"`
	DCV                 float64   `json:"dc_V,omitempty"`
	VehicleSoCFract     float64   `json:"vehicle_soc_fract,omitempty"`
	SessionChargeWh     float64   `json:"session_charge_Wh,omitempty"`
	SessionDischargeWh  float64   `json:"session_discharge_Wh,omitempty"`
	TotalChargeWh       float64   `json:"total_charge_Wh,omitempty"`
	TotalDischargeWh    float64   `json:"total_discharge_Wh,omitempty"`
	LowerLimitW         []float64 `json:"lower_limit_W,omitempty"`
	UpperLimitW         []float64 `json:"upper_limit_W,omitempty"`
	CapacityWh          float64   `json:"capacity_Wh,omitempty"`
	RatedPowerW         float64   `json:"rated_power_W,omitempty"`
	Status              string    `json:"status,omitempty"`
	Protocol            string    `json:"protocol,omitempty"`
	ControlMode         string    `json:"control_mode,omitempty"`
	PlugConnected       bool      `json:"plug_connected,omitempty"`
}
