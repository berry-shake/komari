package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestSQLLogsKeepCredentialValuesOut(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: NewGormLogger().LogMode(gormlogger.Info)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var result string
	if err := db.Raw("SELECT ?", "credential-test-secret").Scan(&result).Error; err != nil {
		t.Fatal(err)
	}
	if result != "credential-test-secret" {
		t.Fatal("query binding changed")
	}
	if strings.Contains(output.String(), result) || !strings.Contains(output.String(), "SELECT [statement omitted]") {
		t.Fatalf("unsafe or missing SQL log: %s", output.String())
	}
}
