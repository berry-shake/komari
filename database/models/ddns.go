package models

import "time"

// DDNS data uses the same SQLite database as the rest of Komari. Credentials
// must never be included in HTTP responses, including settings and record lists.
type DDNSSettings struct {
	ID       uint   `gorm:"primaryKey" json:"-"`
	Enabled  bool   `json:"enabled"`
	Interval int    `json:"interval"`
	Notify   bool   `json:"notify"`
	APIToken string `json:"-"`
}

func (DDNSSettings) TableName() string { return "ddns_settings" }

type DDNSRecord struct {
	ID            string      `gorm:"primaryKey;size:36" json:"id"`
	ZoneID        string      `json:"zone_id"`
	RecordName    string      `gorm:"uniqueIndex:idx_ddns_record_name_type" json:"record_name"`
	RecordType    string      `gorm:"uniqueIndex:idx_ddns_record_name_type" json:"record_type"`
	SourceNodes   StringArray `gorm:"type:text" json:"source_node"`
	TTL           int         `json:"ttl"`
	Proxied       bool        `json:"proxied"`
	Comment       string      `json:"comment"`
	APIToken      string      `json:"-"`
	LastIP        string      `json:"last_ip"`
	LastNode      string      `json:"last_node"`
	LastAction    string      `json:"last_action"`
	LastError     string      `json:"last_error"`
	LastCheckedAt *time.Time  `json:"last_checked_at"`
	LastUpdateAt  *time.Time  `json:"last_update_at"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (DDNSRecord) TableName() string { return "ddns_records" }

type DDNSLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`
	Time       time.Time `json:"time"`
	RecordID   string    `json:"record_id"`
	RecordName string    `json:"record_name"`
	RecordType string    `json:"record_type"`
	Action     string    `json:"action"`
	Success    bool      `json:"success"`
	Detail     string    `json:"detail"`
	IP         string    `json:"ip"`
}

func (DDNSLog) TableName() string { return "ddns_logs" }
