package ddns

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/komari-monitor/komari/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBusy = errors.New("DDNS 正在同步，请稍后重试")
var ErrNotFound = errors.New("DDNS 记录不存在")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{message} }

type SettingsInput struct {
	Enabled    bool    `json:"enabled"`
	Interval   int     `json:"interval"`
	Notify     bool    `json:"notify"`
	APIToken   *string `json:"api_token"`
	ClearToken bool    `json:"clear_api_token"`
}
type SettingsView struct {
	Enabled  bool `json:"enabled"`
	Interval int  `json:"interval"`
	Notify   bool `json:"notify"`
	TokenSet bool `json:"api_token_set"`
	Running  bool `json:"running"`
}
type RecordInput struct {
	ZoneID      string   `json:"zone_id"`
	RecordName  string   `json:"record_name"`
	RecordType  string   `json:"record_type"`
	SourceNodes []string `json:"source_node"`
	TTL         int      `json:"ttl"`
	Proxied     bool     `json:"proxied"`
	Comment     string   `json:"comment"`
	APIToken    *string  `json:"api_token"`
	ClearToken  bool     `json:"clear_api_token"`
}
type RecordView struct {
	models.DDNSRecord
	TokenSet bool `json:"api_token_set"`
}
type Node struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	IPv4   string `json:"ipv4"`
	IPv6   string `json:"ipv6"`
	Online bool   `json:"online"`
}

func (s *Service) settings() (models.DDNSSettings, error) {
	var cfg models.DDNSSettings
	err := s.db.First(&cfg, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cfg = models.DDNSSettings{ID: 1, Interval: 5}
		err = s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&cfg).Error
		if err == nil {
			err = s.db.First(&cfg, 1).Error
		}
	}
	return cfg, err
}
func (s *Service) Settings() (SettingsView, error) {
	cfg, err := s.settings()
	return SettingsView{Enabled: cfg.Enabled, Interval: cfg.Interval, Notify: cfg.Notify, TokenSet: cfg.APIToken != "", Running: s.running.Load()}, err
}
func replaceToken(old string, value *string, clear bool) (string, error) {
	if clear {
		if value != nil && strings.TrimSpace(*value) != "" {
			return "", invalid("清除 Token 与设置新 Token 不能同时使用")
		}
		return "", nil
	}
	if value == nil || strings.TrimSpace(*value) == "" {
		return old, nil
	}
	token := strings.TrimSpace(*value)
	if token == "***SET***" || !validToken(token) {
		return "", invalid("API Token 格式无效")
	}
	return token, nil
}
func (s *Service) SaveSettings(in SettingsInput) error {
	if in.Interval < 1 || in.Interval > 1440 {
		return invalid("检查间隔应为 1 到 1440 分钟")
	}
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	cfg, err := s.settings()
	if err != nil {
		return err
	}
	token, err := replaceToken(cfg.APIToken, in.APIToken, in.ClearToken)
	if err != nil {
		return err
	}
	cfg.Enabled = in.Enabled
	cfg.Interval = in.Interval
	cfg.Notify = in.Notify
	cfg.APIToken = token
	if err = s.db.Save(&cfg).Error; err != nil {
		return err
	}
	select {
	case s.reload <- struct{}{}:
	default:
	}
	return nil
}
func (s *Service) Records() ([]RecordView, error) {
	rows := []models.DDNSRecord{}
	if err := s.db.Order("created_at,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]RecordView, 0, len(rows))
	for _, r := range rows {
		out = append(out, RecordView{DDNSRecord: r, TokenSet: r.APIToken != ""})
	}
	return out, nil
}
func (s *Service) Nodes() ([]Node, error) {
	rows := []models.Client{}
	if err := s.db.Select("uuid,name,ipv4,ipv6").Order("weight desc,name").Find(&rows).Error; err != nil {
		return nil, err
	}
	online := map[string]bool{}
	for _, id := range s.online() {
		online[id] = true
	}
	out := make([]Node, 0, len(rows))
	for _, n := range rows {
		out = append(out, Node{UUID: n.UUID, Name: n.Name, IPv4: n.IPv4, IPv6: n.IPv6, Online: online[n.UUID]})
	}
	return out, nil
}
func (s *Service) SaveRecord(id string, in RecordInput) (RecordView, error) {
	name, err := normalizeName(in.RecordName)
	if err != nil {
		return RecordView{}, invalid(err.Error())
	}
	kind := strings.ToUpper(strings.TrimSpace(in.RecordType))
	if kind == "" {
		kind = "A"
	}
	if kind != "A" && kind != "AAAA" {
		return RecordView{}, invalid("记录类型仅支持 A 或 AAAA")
	}
	zoneID := strings.TrimSpace(in.ZoneID)
	if zoneID != "" && !validID(zoneID) {
		return RecordView{}, invalid("Zone ID 必须为 32 位十六进制字符串，或留空自动识别")
	}
	ttl := in.TTL
	if ttl == 0 {
		ttl = 1
	}
	if ttl != 1 && (ttl < 60 || ttl > 86400) {
		return RecordView{}, invalid("TTL 应为 1（自动）或 60 到 86400 秒")
	}
	if in.Proxied {
		ttl = 1
	}
	if len(in.SourceNodes) == 0 || len(in.SourceNodes) > 32 {
		return RecordView{}, invalid("请选择 1 到 32 个来源节点")
	}
	if len(in.Comment) > 1000 {
		return RecordView{}, invalid("备注不能超过 1000 字节")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range in.SourceNodes {
		if _, err := uuid.Parse(id); err != nil {
			return RecordView{}, invalid("来源节点 UUID 格式无效")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if !s.mu.TryLock() {
		return RecordView{}, ErrBusy
	}
	defer s.mu.Unlock()
	var n int64
	if err = s.db.Model(&models.Client{}).Where("uuid IN ?", ids).Count(&n).Error; err != nil {
		return RecordView{}, err
	}
	if n != int64(len(ids)) {
		return RecordView{}, invalid("来源节点不存在，请刷新节点列表")
	}
	old := models.DDNSRecord{}
	if id != "" {
		if err = s.db.First(&old, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return RecordView{}, ErrNotFound
		} else if err != nil {
			return RecordView{}, err
		}
	}
	if err = s.db.Model(&models.DDNSRecord{}).Where("record_name = ? AND record_type = ? AND id <> ?", name, kind, id).Count(&n).Error; err != nil {
		return RecordView{}, err
	}
	if n > 0 {
		return RecordView{}, invalid("已存在相同名称和类型的 DDNS 记录")
	}
	if id == "" {
		if err = s.db.Model(&models.DDNSRecord{}).Count(&n).Error; err != nil {
			return RecordView{}, err
		}
		if n >= 500 {
			return RecordView{}, invalid("最多支持 500 条 DDNS 记录")
		}
	}
	token, err := replaceToken(old.APIToken, in.APIToken, in.ClearToken)
	if err != nil {
		return RecordView{}, err
	}
	row := old
	row.RecordName = name
	row.RecordType = kind
	row.ZoneID = zoneID
	row.SourceNodes = ids
	row.TTL = ttl
	row.Proxied = in.Proxied
	row.Comment = strings.TrimSpace(in.Comment)
	row.APIToken = token
	action := "edit"
	if id == "" {
		row.ID = uuid.NewString()
		action = "add"
	}
	if old.RecordName != name || old.RecordType != kind || old.ZoneID != zoneID {
		row.LastIP = ""
		row.LastNode = ""
		row.LastCheckedAt = nil
		row.LastUpdateAt = nil
	}
	row.LastAction = "pending"
	row.LastError = ""
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return appendLog(tx, models.DDNSLog{RecordID: row.ID, RecordName: name, RecordType: kind, Action: action, Success: true, Detail: "保存 DDNS 记录"})
	})
	return RecordView{DDNSRecord: row, TokenSet: token != ""}, err
}
func (s *Service) DeleteRecord(id string) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	var row models.DDNSRecord
	if err := s.db.First(&row, "id = ?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return appendLog(tx, models.DDNSLog{RecordID: id, RecordName: row.RecordName, RecordType: row.RecordType, Action: "delete", Success: true, Detail: "移除 DDNS 配置；Cloudflare DNS 记录保留"})
	})
}
func appendLog(db *gorm.DB, row models.DDNSLog) error {
	row.Time = time.Now().UTC()
	if err := db.Create(&row).Error; err != nil {
		return err
	}
	return db.Where("id NOT IN (?)", db.Model(&models.DDNSLog{}).Select("id").Order("id desc").Limit(500)).Delete(&models.DDNSLog{}).Error
}

type LogView struct {
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
	Logs       []models.DDNSLog `json:"logs"`
}

func (s *Service) Logs(name, action string, page, pageSize int) (LogView, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 500 {
		pageSize = 500
	}
	out := LogView{Page: page, PageSize: pageSize, TotalPages: 1, Logs: []models.DDNSLog{}}
	// Count and rows use the same snapshot if a sync trims logs or another
	// administrator clears them while this page is being read.
	err := s.db.Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&models.DDNSLog{})
		if name != "" {
			q = q.Where("record_name = ?", name)
		}
		if action != "" {
			q = q.Where("action = ?", action)
		}
		if err := q.Count(&out.Total).Error; err != nil {
			return err
		}
		if out.Total > 0 {
			out.TotalPages = int((out.Total-1)/int64(pageSize)) + 1
		}
		// Clamp before calculating the offset, including an empty result set.
		if out.Page > out.TotalPages {
			out.Page = out.TotalPages
		}
		return q.Order("id desc").Offset((out.Page - 1) * pageSize).Limit(pageSize).Find(&out.Logs).Error
	})
	return out, err
}
func (s *Service) ClearLogs() error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	return s.db.Where("1 = 1").Delete(&models.DDNSLog{}).Error
}
func describeError(err error, token string) string { return redact(fmt.Sprint(err), token) }
