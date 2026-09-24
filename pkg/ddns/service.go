package ddns

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/utils/messageSender"
	agentruntime "github.com/komari-monitor/komari/web/agent"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Service struct {
	db      *gorm.DB
	cf      *cloudflare
	online  func() []string
	notify  func(models.EventMessage) error
	mu      sync.Mutex
	running atomic.Bool
	start   sync.Once
	reload  chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
}

var defaultOnce sync.Once
var defaultService *Service

func Default() *Service {
	defaultOnce.Do(func() {
		defaultService = New(dbcore.GetDBInstance(), agentruntime.GetAllOnlineUUIDs)
		defaultService.notify = messageSender.SendEvent
	})
	return defaultService
}
func New(db *gorm.DB, online func() []string) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	if online == nil {
		online = func() []string { return nil }
	}
	// GORM's SQL diagnostics can include bound credentials. DDNS errors are
	// translated by the API instead of printing SQL statements with tokens.
	return &Service{db: db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}), cf: newCloudflare(), online: online, reload: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}
func (s *Service) Start() { s.start.Do(func() { go s.schedule() }) }
func (s *Service) Stop()  { s.cancel() }
func (s *Service) schedule() {
	for {
		cfg, err := s.settings()
		interval := 5
		if err == nil && cfg.Interval >= 1 && cfg.Interval <= 1440 {
			interval = cfg.Interval
		}
		timer := time.NewTimer(time.Duration(interval) * time.Minute)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.reload:
			timer.Stop()
			continue
		case <-timer.C:
			// Re-read before a scheduled write: disabling takes effect immediately.
			cfg, err = s.settings()
			if err == nil && cfg.Enabled {
				if _, err = s.sync(s.ctx, true); err != nil && err != ErrBusy {
					log.Println("DDNS scheduled sync failed")
				}
			}
		}
	}
}

type SyncResult struct {
	ID         string `json:"id"`
	RecordName string `json:"record_name"`
	RecordType string `json:"record_type"`
	Action     string `json:"action"`
	IP         string `json:"ip,omitempty"`
	Error      string `json:"error,omitempty"`
}
type SyncReport struct {
	OK      bool         `json:"ok"`
	Total   int          `json:"total"`
	Changed int          `json:"changed"`
	Skipped int          `json:"skipped"`
	Errors  int          `json:"errors"`
	Records []SyncResult `json:"records"`
}

func (s *Service) Sync(parent context.Context) (SyncReport, error) {
	return s.sync(parent, false)
}
func (s *Service) sync(parent context.Context, scheduled bool) (SyncReport, error) {
	result := SyncReport{OK: true, Records: []SyncResult{}}
	if !s.mu.TryLock() {
		return result, ErrBusy
	}
	defer s.mu.Unlock()
	s.running.Store(true)
	defer s.running.Store(false)
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	cfg, err := s.settings()
	if err != nil {
		return result, err
	}
	if scheduled && !cfg.Enabled {
		return result, nil
	}
	records := []models.DDNSRecord{}
	if err = s.db.Order("created_at,id").Find(&records).Error; err != nil {
		return result, err
	}
	nodes, err := s.Nodes()
	if err != nil {
		return result, err
	}
	byID := map[string]Node{}
	for _, n := range nodes {
		byID[n.UUID] = n
	}
	cache := map[string][]zone{}
	for _, r := range records {
		recordResult := s.syncRecord(ctx, cfg, r, byID, cache)
		result.Records = append(result.Records, recordResult)
		switch recordResult.Action {
		case "create", "update":
			result.Changed++
		case "skip":
			result.Skipped++
		default:
			result.Errors++
		}
	}
	result.Total = len(records)
	result.OK = result.Errors == 0
	detail := fmt.Sprintf("同步完成：共 %d 条，创建/更新 %d 条，跳过 %d 条，失败 %d 条", result.Total, result.Changed, result.Skipped, result.Errors)
	if err = appendLog(s.db, models.DDNSLog{Action: "sync", Success: result.OK, Detail: detail}); err != nil {
		return result, err
	}
	return result, nil
}
func targetNode(r models.DDNSRecord, nodes map[string]Node) (Node, string) {
	for _, id := range r.SourceNodes {
		n, ok := nodes[id]
		if !ok || !n.Online {
			continue
		}
		value := n.IPv4
		if r.RecordType == "AAAA" {
			value = n.IPv6
		}
		if ip := publicIP(value, r.RecordType); ip != "" {
			return n, ip
		}
	}
	return Node{}, ""
}
func (s *Service) syncRecord(ctx context.Context, cfg models.DDNSSettings, row models.DDNSRecord, nodes map[string]Node, cache map[string][]zone) SyncResult {
	result := SyncResult{ID: row.ID, RecordName: row.RecordName, RecordType: row.RecordType, Action: "error"}
	token := row.APIToken
	if token == "" {
		token = cfg.APIToken
	}
	detail := ""
	node := Node{}
	perform := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if token == "" {
			return fmt.Errorf("未配置 Cloudflare API Token")
		}
		var ip string
		node, ip = targetNode(row, nodes)
		if ip == "" {
			result.Action = "skip"
			detail = "无在线节点提供可用的公网 IP"
			return nil
		}
		zoneID := row.ZoneID
		if zoneID == "" {
			zones, ok := cache[token]
			if !ok {
				var err error
				zones, err = s.cf.zones(ctx, token)
				if err != nil {
					return err
				}
				cache[token] = zones
			}
			zoneID = resolveZone(zones, row.RecordName)
			if zoneID == "" {
				return fmt.Errorf("未找到匹配域名，请检查 Token 的 Zone:Read 权限或手动填写 Zone ID")
			}
		}
		if !validID(zoneID) {
			return fmt.Errorf("Cloudflare Zone ID 无效")
		}
		existing, err := s.cf.lookup(ctx, token, zoneID, row.RecordName, row.RecordType)
		if err != nil {
			return err
		}
		ttl := row.TTL
		if row.Proxied {
			ttl = 1
		}
		payload := dnsRecord{Type: row.RecordType, Name: row.RecordName, Content: ip, TTL: ttl, Proxied: row.Proxied}
		id := ""
		action := "create"
		if existing != nil {
			id = existing.ID
			action = "update"
			if sameIP(existing.Content, ip) && existing.Proxied == row.Proxied && (row.Proxied || existing.TTL == ttl) {
				result.Action = "skip"
				result.IP = ip
				detail = "IP、代理状态和 TTL 均无变化"
				return nil
			}
		}
		if err = s.cf.write(ctx, token, zoneID, id, payload); err != nil {
			return err
		}
		result.Action = action
		result.IP = ip
		detail = "DNS 记录已同步到 " + ip
		return nil
	}
	if err := perform(); err != nil {
		result.Error = describeError(err, token)
		detail = result.Error
	}
	now := time.Now().UTC()
	row.LastCheckedAt = &now
	row.LastAction = result.Action
	row.LastError = result.Error
	if result.IP != "" {
		row.LastIP = result.IP
		row.LastNode = node.Name
	}
	if result.Action == "create" || result.Action == "update" {
		row.LastUpdateAt = &now
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return appendLog(tx, models.DDNSLog{RecordID: row.ID, RecordName: row.RecordName, RecordType: row.RecordType, Action: result.Action, Success: result.Error == "", Detail: detail, IP: result.IP})
	})
	if err != nil {
		result.Action = "error"
		result.Error = "保存 DDNS 同步状态失败"
		return result
	}
	if (result.Action == "create" || result.Action == "update") && cfg.Notify && s.notify != nil {
		event := models.EventMessage{Event: "DDNS", Emoji: "🌐", Time: now, Clients: []models.Client{{UUID: node.UUID, Name: node.Name}}, Message: fmt.Sprintf("DDNS %s：%s → %s（节点 %s）", result.Action, row.RecordName, result.IP, node.Name)}
		if err := s.notify(event); err != nil {
			_ = appendLog(s.db, models.DDNSLog{RecordID: row.ID, RecordName: row.RecordName, RecordType: row.RecordType, Action: "notify", Success: false, Detail: "DNS 同步成功，发送通知失败"})
		}
	}
	return result
}
