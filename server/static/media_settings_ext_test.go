package static

import (
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateIndexInjectsMediaSettingsExtIntoBothManageAndIndexHtml(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.SettingItem{}); err != nil {
		t.Fatal(err)
	}

	oldConf, oldDir := conf.Conf, conf.DataDir
	oldRaw, oldManage, oldIndex := conf.RawIndexHtml, conf.ManageHtml, conf.IndexHtml
	conf.Conf = conf.DefaultConfig(t.TempDir())
	conf.DataDir = t.TempDir()
	db.Init(database)
	t.Cleanup(func() {
		conf.Conf, conf.DataDir = oldConf, oldDir
		conf.RawIndexHtml, conf.ManageHtml, conf.IndexHtml = oldRaw, oldManage, oldIndex
	})

	if err := database.Create(&model.SettingItem{
		Key:   conf.CustomizeBody,
		Value: `<div id="custom-floating-bar"></div>`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	conf.RawIndexHtml = `<!doctype html><html><head><!-- customize head --><title>Loading...</title></head><body><div id="root"></div><!-- customize body --></body></html>`
	UpdateIndex()

	const marker = `id="openlist-media-date-added-ext"`
	if cnt := strings.Count(conf.ManageHtml, marker); cnt != 1 {
		t.Fatalf("expected ManageHtml to contain %s once, got %d", marker, cnt)
	}
	if cnt := strings.Count(conf.IndexHtml, marker); cnt != 1 {
		t.Fatalf("expected IndexHtml to contain %s once (for SPA routing from / to /@manage/media/settings), got %d", marker, cnt)
	}
	if !strings.Contains(conf.IndexHtml, `id="custom-floating-bar"`) {
		t.Fatalf("expected IndexHtml to preserve CustomizeBody")
	}

	// Idempotency check when UpdateIndex runs again
	UpdateIndex()
	if cnt := strings.Count(conf.IndexHtml, marker); cnt != 1 {
		t.Fatalf("expected IndexHtml to still contain %s once after repeated UpdateIndex, got %d", marker, cnt)
	}
}
