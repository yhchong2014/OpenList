package handles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/gin-gonic/gin"
)

func TestSetMediaDateAddedFollowReleaseAdminSyncsExistingAndEmby(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := setupMediaReviewTest(t)
	if err := database.AutoMigrate(&model.SettingItem{}, &model.MediaConfig{}, &model.Meta{}, &model.MediaItem{}, &model.MediaUserData{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.SetMediaDateAddedFollowRelease(false)
	})
	if err := database.Create(&model.MediaConfig{MediaType: model.MediaTypeVideo, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}

	scanTime := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	movie := &model.MediaItem{
		CreatedAt:   scanTime,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/movies",
		FileName:    "Dune (2021).mkv",
		ScrapedName: "沙丘",
		ReleaseDate: "2021-10-22",
		VideoType:   "movie",
	}
	if err := database.Create(movie).Error; err != nil {
		t.Fatal(err)
	}

	// 调用后台一键开关端点开启同步
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/media/date_added_follow_release", bytes.NewBufferString(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	SetMediaDateAddedFollowReleaseAdmin(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Enabled bool  `json:"enabled"`
			Synced  int64 `json:"synced"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 200 || !resp.Data.Enabled || resp.Data.Synced != 1 {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	if !setting.GetBool(conf.MediaDateAddedFollowRelease) {
		t.Fatalf("expected setting %s to be true", conf.MediaDateAddedFollowRelease)
	}

	// 验证 Emby 电影条目的 DateCreated 已变为 2021-10-22
	user := &model.User{ID: 1, Username: "admin", Role: model.ADMIN}
	lib := loadEmbyLibrary(user)
	items := lib.childrenOf("lib-movie")
	if len(items) != 1 {
		t.Fatalf("childrenOf(lib-movie) len = %d, want 1", len(items))
	}
	if !strings.HasPrefix(items[0].DateCreated, "2021-10-22") {
		t.Fatalf("Emby DateCreated = %q, want prefix 2021-10-22", items[0].DateCreated)
	}
}
