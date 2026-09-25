package ddns

import (
	"testing"
	"time"

	"github.com/komari-monitor/komari/database/models"
)

func TestLogPaginationAndFilters(t *testing.T) {
	s := setup(t, false)
	// Identical timestamps exercise the stable ID ordering at page boundaries.
	rows := make([]models.DDNSLog, 53)
	for i := range rows {
		rows[i] = models.DDNSLog{Time: time.Unix(100, 0), RecordName: "a.example.com", Action: "skip"}
		if i%2 == 0 {
			rows[i].RecordName = "b.example.com"
			rows[i].Action = "update"
		}
	}
	if err := s.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	for page := 1; page <= 3; page++ {
		got, err := s.Logs("", "", page, 20)
		wantRows := 20
		if page == 3 {
			wantRows = 13
		}
		if err != nil || got.Total != 53 || got.Page != page || got.PageSize != 20 || got.TotalPages != 3 || len(got.Logs) != wantRows {
			t.Fatalf("page %d: %+v, %v", page, got, err)
		}
		for i, row := range got.Logs {
			if seen[row.ID] || row.ID != uint64(53-(page-1)*20-i) {
				t.Fatalf("duplicate or out of order log: %+v", row)
			}
			seen[row.ID] = true
		}
	}
	filtered, err := s.Logs("b.example.com", "update", 2, 20)
	if err != nil || filtered.Total != 27 || filtered.TotalPages != 2 || len(filtered.Logs) != 7 {
		t.Fatalf("filtered page: %+v %v", filtered, err)
	}
	for _, row := range filtered.Logs {
		if row.RecordName != "b.example.com" || row.Action != "update" {
			t.Fatal("filter leaked another record or action")
		}
	}
	last, err := s.Logs("", "", int(^uint(0)>>1), 20)
	if err != nil || last.Page != 3 || len(last.Logs) != 13 {
		t.Fatalf("out of range page: %+v %v", last, err)
	}
	bounded, err := s.Logs("", "", 0, 10000)
	if err != nil || bounded.Page != 1 || bounded.PageSize != 500 || len(bounded.Logs) != 53 {
		t.Fatalf("bounded size: %+v %v", bounded, err)
	}
	empty, err := s.Logs("b.example.com", "skip", 2, 20)
	if err != nil || empty.Page != 1 || empty.TotalPages != 1 || empty.Total != 0 || empty.Logs == nil || len(empty.Logs) != 0 {
		t.Fatalf("empty filtered page: %+v %v", empty, err)
	}
	if err := s.ClearLogs(); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.Logs("", "", 3, 20)
	if err != nil || cleared.Page != 1 || cleared.Total != 0 || len(cleared.Logs) != 0 {
		t.Fatalf("page after clear: %+v %v", cleared, err)
	}
}
