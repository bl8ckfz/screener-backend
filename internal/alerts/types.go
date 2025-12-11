package alerts

import "time"

// AlertRule represents a rule configuration from the database
type AlertRule struct {
	RuleType    string                 `json:"rule_type"`
	Config      map[string]interface{} `json:"config"`
	Description string                 `json:"description"`
}

// AlertCriteria contains the evaluation criteria from rule config
type AlertCriteria struct {
	// Price change thresholds (percentages)
	Change1hMin  *float64 `json:"change_1h_min,omitempty"`
	Change1hMax  *float64 `json:"change_1h_max,omitempty"`
	Change5mMin  *float64 `json:"change_5m_min,omitempty"`
	Change5mMax  *float64 `json:"change_5m_max,omitempty"`
	Change15mMin *float64 `json:"change_15m_min,omitempty"`
	Change15mMax *float64 `json:"change_15m_max,omitempty"`
	Change8hMin  *float64 `json:"change_8h_min,omitempty"`
	Change8hMax  *float64 `json:"change_8h_max,omitempty"`
	Change1dMin  *float64 `json:"change_1d_min,omitempty"`
	Change1dMax  *float64 `json:"change_1d_max,omitempty"`

	// Progressive comparison flags
	Change8hGt1h  bool `json:"change_8h_gt_1h,omitempty"`
	Change1dGt8h  bool `json:"change_1d_gt_8h,omitempty"`
	Change8hLt1h  bool `json:"change_8h_lt_1h,omitempty"`
	Change1dLt8h  bool `json:"change_1d_lt_8h,omitempty"`
	Change15mGt5m bool `json:"change_15m_gt_5m,omitempty"`
	Change1hGt15m bool `json:"change_1h_gt_15m,omitempty"`
	Change1hLt15m bool `json:"change_1h_lt_15m,omitempty"`
	Change8hGt1hAlt bool `json:"change_8h_gt_1h_alt,omitempty"` // For 15m rules
	Change15mLt5m bool `json:"change_15m_lt_5m,omitempty"`
	Change1hLt15mAlt bool `json:"change_1h_lt_15m_alt,omitempty"`

	// Volume thresholds
	Volume1hMin  *float64 `json:"volume_1h_min,omitempty"`
	Volume5mMin  *float64 `json:"volume_5m_min,omitempty"`
	Volume15mMin *float64 `json:"volume_15m_min,omitempty"`
	Volume8hMin  *float64 `json:"volume_8h_min,omitempty"`

	// Volume ratios
	VolumeRatio1h8h  *float64 `json:"volume_ratio_1h_8h,omitempty"`
	VolumeRatio1h1d  *float64 `json:"volume_ratio_1h_1d,omitempty"`
	VolumeRatio5m15m *float64 `json:"volume_ratio_5m_15m,omitempty"`
	VolumeRatio5m1h  *float64 `json:"volume_ratio_5m_1h,omitempty"`
	VolumeRatio5m8h  *float64 `json:"volume_ratio_5m_8h,omitempty"`
	VolumeRatio15m1h *float64 `json:"volume_ratio_15m_1h,omitempty"`
	VolumeRatio15m8h *float64 `json:"volume_ratio_15m_8h,omitempty"`

	// Change acceleration multipliers
	ChangeAcceleration *float64 `json:"change_acceleration,omitempty"`

	// Market cap filters
	MarketCapMin *float64 `json:"market_cap_min,omitempty"`
	MarketCapMax *float64 `json:"market_cap_max,omitempty"`
}

// Alert represents a triggered alert
type Alert struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	RuleType    string    `json:"rule_type"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Price       float64   `json:"price"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// Metrics represents the calculated metrics from metrics-calculator
type Metrics struct {
	Symbol         string    `json:"symbol"`
	Timestamp      time.Time `json:"timestamp"`
	LastPrice      float64   `json:"last_price"`
	VCP            float64   `json:"vcp"`
	PriceChange5m  float64   `json:"price_change_5m"`
	PriceChange15m float64   `json:"price_change_15m"`
	PriceChange1h  float64   `json:"price_change_1h"`
	PriceChange8h  float64   `json:"price_change_8h"`
	PriceChange1d  float64   `json:"price_change_1d"`
	Volume5m       float64   `json:"volume_5m"`
	Volume15m      float64   `json:"volume_15m"`
	Volume1h       float64   `json:"volume_1h"`
	Volume8h       float64   `json:"volume_8h"`
	Volume24h      float64   `json:"volume_24h"`
	RSI            float64   `json:"rsi"`
}
