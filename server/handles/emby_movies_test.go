package handles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
)

func TestEmbyMultiVersionMovieMerging(t *testing.T) {
	now := time.Now()
	videos := []model.MediaItem{
		// 1) 未刮削同目录多版本电影：Movies - 1080p + Movies - 4K -> 应合并为 1 部 "Movies"
		{
			ID:          1,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/movies",
			FileName:    "Movies - 1080p.mkv",
			ScrapedName: "Movies - 1080p",
			FileSize:    2_000_000_000,
			Duration:    7200,
			CreatedAt:   now,
		},
		{
			ID:          2,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/movies",
			FileName:    "Movies - 4K.mp4",
			ScrapedName: "Movies - 4K",
			FileSize:    8_000_000_000,
			Duration:    7200,
			CreatedAt:   now.Add(time.Minute),
		},
		// 2) 已刮削跨目录同 TMDB ID 电影 -> 应合并为 1 部 "猫和老鼠"
		{
			ID:          3,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/disk1/movies",
			FileName:    "Tom.and.Jerry.2021.1080p.mkv",
			ScrapedName: "猫和老鼠",
			ExternalID:  "tmdb:587807",
			VideoType:   "movie",
			FileSize:    3_000_000_000,
			Duration:    6060,
			ScrapedAt:   &now,
		},
		{
			ID:          4,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/disk2/remux",
			FileName:    "Tom.and.Jerry.2021.2160p.Remux.mkv",
			ScrapedName: "猫和老鼠",
			ExternalID:  "tmdb:587807",
			VideoType:   "movie",
			FileSize:    18_000_000_000,
			Duration:    6060,
			ScrapedAt:   &now,
		},
		// 3) 正常系列电影（非版本词）：Dune - Part 1 与 Dune - Part 2 必须保持独立不合并
		{
			ID:          5,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/movies",
			FileName:    "Dune - Part 1.mkv",
			ScrapedName: "Dune - Part 1",
		},
		{
			ID:          6,
			MediaType:   model.MediaTypeVideo,
			FolderPath:  "/movies",
			FileName:    "Dune - Part 2.mkv",
			ScrapedName: "Dune - Part 2",
		},
	}

	lib := &embyLibrary{
		user:     &model.User{ID: 1, Username: "alice"},
		videos:   videos,
		userData: map[[2]uint]*model.MediaUserData{},
		ids:      map[string]string{},
	}
	lib.registerIDs()

	children := lib.childrenOf("lib-movie")
	if len(children) != 4 {
		t.Fatalf("childrenOf(lib-movie) len = %d, want 4 (2 merged multi-version + 2 standalone Dune parts)", len(children))
	}

	byName := map[string]*embyItem{}
	for _, it := range children {
		byName[it.Name] = it
	}

	mvUnscraped := byName["Movies"]
	if mvUnscraped == nil {
		t.Fatalf("merged unscraped movie 'Movies' not found in %+v", byName)
	}
	if mvUnscraped.MediaSourceCount != 2 || len(mvUnscraped.MediaSources) != 2 {
		t.Fatalf("Movies MediaSources len = %d (count=%d), want 2", len(mvUnscraped.MediaSources), mvUnscraped.MediaSourceCount)
	}
	if mvUnscraped.MediaSources[0].Name != "1080p" || mvUnscraped.MediaSources[1].Name != "4K" {
		t.Fatalf("Movies version labels = [%q, %q], want [\"1080p\", \"4K\"]",
			mvUnscraped.MediaSources[0].Name, mvUnscraped.MediaSources[1].Name)
	}
	if mvUnscraped.MediaSources[0].Id == mvUnscraped.MediaSources[1].Id {
		t.Fatalf("Movies MediaSource IDs must be distinct, both were %q", mvUnscraped.MediaSources[0].Id)
	}

	mvScraped := byName["猫和老鼠"]
	if mvScraped == nil || len(mvScraped.MediaSources) != 2 {
		t.Fatalf("merged TMDB movie '猫和老鼠' missing or wrong MediaSources: %+v", mvScraped)
	}
	if mvScraped.ProviderIds["Tmdb"] != "587807" {
		t.Fatalf("ProviderIds[Tmdb] = %q, want 587807", mvScraped.ProviderIds["Tmdb"])
	}

	if byName["Dune - Part 1"] == nil || byName["Dune - Part 2"] == nil {
		t.Fatalf("Dune - Part 1 / Part 2 should remain separate movies, got %+v", byName)
	}
}

func TestEmbyMultiVersionPlaybackSelectionAndFolderMovie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	folderMovie := model.MediaItem{
		ID:          20,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/movies",
		FileName:    "Avatar (2009)",
		ScrapedName: "Avatar (2009)",
		IsFolder:    true,
		Episodes: `[
			{"file_name":"Avatar (2009) - 1080p.mkv","index":1,"season":0},
			{"file_name":"Avatar (2009) - 4K.mkv","index":2,"season":0}
		]`,
	}

	lib := &embyLibrary{
		user:     &model.User{ID: 1, Username: "alice"},
		videos:   []model.MediaItem{folderMovie},
		userData: map[[2]uint]*model.MediaUserData{},
		ids:      map[string]string{},
	}
	lib.registerIDs()

	// 未标 VideoType 的纯多版本电影目录不应被当成剧集
	if len(lib.seriesList()) != 0 {
		t.Fatalf("seriesList len = %d, want 0 for multi-version movie folder", len(lib.seriesList()))
	}
	movies := lib.childrenOf("lib-movie")
	if len(movies) != 1 || len(movies[0].MediaSources) != 2 {
		t.Fatalf("folder movie items = %+v, want 1 movie with 2 MediaSources", movies)
	}

	src1080 := movies[0].MediaSources[0]
	src4K := movies[0].MediaSources[1]
	if src1080.Name != "1080p" || src4K.Name != "4K" {
		t.Fatalf("folder movie version names = [%q, %q], want [1080p, 4K]", src1080.Name, src4K.Name)
	}

	// 验证每个版本 ID 都能精确解析到目录内对应的具体视频文件（而非目录路径）
	t4K, ok := lib.playTarget(src4K.Id)
	if !ok || t4K.RawPath != "/movies/Avatar (2009)/Avatar (2009) - 4K.mkv" {
		t.Fatalf("playTarget(4K) = (%+v, %v), want RawPath='/movies/Avatar (2009)/Avatar (2009) - 4K.mkv'", t4K, ok)
	}

	// 验证 PlaybackInfo 在传入 MediaSourceId=src4K.Id 时把 4K 源排在首位
	bodyBytes, _ := json.Marshal(map[string]string{"MediaSourceId": src4K.Id})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/@emby/Items/"+movies[0].Id+"/PlaybackInfo", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	embyPlaybackInfo(c, lib, movies[0].Id)
	if w.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo status = %d, want 200", w.Code)
	}
	var resp struct {
		MediaSources []embyMediaSource `json:"MediaSources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal PlaybackInfo: %v", err)
	}
	if len(resp.MediaSources) != 2 || resp.MediaSources[0].Id != src4K.Id {
		t.Fatalf("PlaybackInfo MediaSources[0] = %+v, want selected 4K Id=%s", resp.MediaSources, src4K.Id)
	}
}

func TestEmbyMultiVersionUserDataAggregation(t *testing.T) {
	database := setupMediaReviewTest(t)
	if err := database.AutoMigrate(&model.MediaItem{}, &model.MediaUserData{}); err != nil {
		t.Fatal(err)
	}

	m1 := &model.MediaItem{
		MediaType:  model.MediaTypeVideo,
		FolderPath: "/movies",
		FileName:   "Interstellar - 1080p.mkv",
		ExternalID: "tmdb:157336",
		VideoType:  "movie",
		Duration:   1000,
	}
	m2 := &model.MediaItem{
		MediaType:  model.MediaTypeVideo,
		FolderPath: "/movies",
		FileName:   "Interstellar - 4K.mkv",
		ExternalID: "tmdb:157336",
		VideoType:  "movie",
		Duration:   1000,
	}
	if err := database.Create(m1).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(m2).Error; err != nil {
		t.Fatal(err)
	}

	// 旧状态：m2 曾有续播进度 300s
	now := time.Now()
	_ = db.SaveMediaUserData(&model.MediaUserData{
		UserID:        9,
		ItemID:        m2.ID,
		PositionTicks: 3_000_000_000,
		IsFavorite:    true,
		LastPlayedAt:  &now,
	})

	user := &model.User{ID: 9, Username: "carol", Role: model.ADMIN}
	lib := loadEmbyLibrary(user)
	items := lib.childrenOf("lib-movie")
	if len(items) != 1 {
		t.Fatalf("childrenOf(lib-movie) len = %d, want 1", len(items))
	}
	if items[0].UserData.PlaybackPositionTicks != 3_000_000_000 || !items[0].UserData.IsFavorite {
		t.Fatalf("merged UserData = %+v, want PositionTicks=3000000000 IsFavorite=true", items[0].UserData)
	}

	// 标记整部合并电影为已看 -> m2 上的残留续播点也应同步清零
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/@emby/Users/9/PlayedItems/"+items[0].Id, nil)
	embySetPlayed(c, user, items[0].Id, true)

	libAfter := loadEmbyLibrary(user)
	afterItems := libAfter.childrenOf("lib-movie")
	if !afterItems[0].UserData.Played || afterItems[0].UserData.PlaybackPositionTicks != 0 {
		t.Fatalf("after mark played UserData = %+v, want Played=true PositionTicks=0", afterItems[0].UserData)
	}
}
