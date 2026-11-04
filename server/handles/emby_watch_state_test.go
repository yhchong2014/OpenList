package handles

import (
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestSeriesItemAggregatesUserData(t *testing.T) {
	show := model.MediaItem{
		ID:          10,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/tv",
		FileName:    "漫长的季节",
		ScrapedName: "漫长的季节",
		IsFolder:    true,
		VideoType:   "tv",
		Episodes: `[
			{"file_name":"S01E01.mkv","index":1,"season":1},
			{"file_name":"S01E02.mkv","index":2,"season":1}
		]`,
	}
	user := &model.User{ID: 1, Username: "alice"}
	lib := &embyLibrary{
		user:   user,
		videos: []model.MediaItem{show},
		userData: map[[2]uint]*model.MediaUserData{
			{10, 0}: {UserID: 1, ItemID: 10, EpisodeIndex: 0, IsFavorite: true},
			{10, 1}: {UserID: 1, ItemID: 10, EpisodeIndex: 1, Played: true, PlayCount: 1},
		},
		ids: map[string]string{},
	}

	sList := lib.seriesList()
	if len(sList) != 1 {
		t.Fatalf("seriesList len = %d, want 1", len(sList))
	}
	sr := lib.seriesItem(sList[0])
	if sr.UserData.UnplayedItemCount != 1 || sr.UserData.Played || sr.UserData.PlayedPercentage != 50 || !sr.UserData.IsFavorite {
		t.Fatalf("partial series UserData = %+v, want Unplayed=1 Played=false Pct=50 IsFavorite=true", sr.UserData)
	}

	// 第 2 集看完后，剧集与季都应标记 Played=true、UnplayedItemCount=0
	lib.userData[[2]uint{10, 2}] = &model.MediaUserData{UserID: 1, ItemID: 10, EpisodeIndex: 2, Played: true, PlayCount: 1}
	srDone := lib.seriesItem(sList[0])
	if !srDone.UserData.Played || srDone.UserData.UnplayedItemCount != 0 || srDone.UserData.PlayedPercentage != 100 {
		t.Fatalf("completed series UserData = %+v, want Played=true Unplayed=0 Pct=100", srDone.UserData)
	}
	snDone := lib.seasonItem(sList[0], 1)
	if !snDone.UserData.Played || snDone.UserData.UnplayedItemCount != 0 || snDone.UserData.PlayedPercentage != 100 {
		t.Fatalf("completed season UserData = %+v, want Played=true Unplayed=0 Pct=100", snDone.UserData)
	}
}

func TestApplyProgressAutoMarksWatchedAndPreservesResume(t *testing.T) {
	database := setupMediaReviewTest(t)
	if err := database.AutoMigrate(&model.MediaItem{}, &model.MediaUserData{}); err != nil {
		t.Fatal(err)
	}

	fixedUpdated := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	movie := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/movies",
		FileName:    "test.mkv",
		ScrapedName: "Test Movie",
		Duration:    0,
		UpdatedAt:   fixedUpdated,
	}
	if err := database.Create(movie).Error; err != nil {
		t.Fatal(err)
	}
	// 固定 updated_at，确保回填 duration 不会破坏 Subsonic Starred 依赖的 updated_at
	database.Model(movie).UpdateColumn("updated_at", fixedUpdated)

	user := &model.User{ID: 7, Username: "bob"}
	target := &embyPlayTarget{
		Item:     movie,
		ItemID:   movie.ID,
		Duration: movie.Duration,
	}

	// 1. 播放到 40%（客户端上报 RunTimeTicks=100s），随后停止但未传 PositionTicks -> 应保留 40% 续播点且回填 Duration=100
	applyProgressWithRuntime(user, target, 400_000_000, true, 1_000_000_000, false)
	applyProgressWithRuntime(user, target, 0, false, 1_000_000_000, true)

	ud, err := db.GetMediaUserData(user.ID, movie.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ud.Played || ud.PositionTicks != 400_000_000 {
		t.Fatalf("mid-movie stop UserData = %+v, want Played=false PositionTicks=400000000", ud)
	}
	reloaded, err := db.GetMediaItemByID(movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Duration != 100 {
		t.Fatalf("expected backfilled Duration=100, got %d", reloaded.Duration)
	}
	if !reloaded.UpdatedAt.Equal(fixedUpdated) {
		t.Fatalf("UpdatedAt changed from %v to %v", fixedUpdated, reloaded.UpdatedAt)
	}

	// 2. 进度推进到 95%，随后收到 EOF Stopped(PositionTicks=0) -> 自动标记已看、清零续播点、PlayCount 只加 1 次
	applyProgressWithRuntime(user, target, 950_000_000, true, 0, false)
	applyProgressWithRuntime(user, target, 0, false, 0, true)

	ud2, err := db.GetMediaUserData(user.ID, movie.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ud2.Played || ud2.PositionTicks != 0 || ud2.PlayCount != 1 {
		t.Fatalf("95%% EOF UserData = %+v, want Played=true PositionTicks=0 PlayCount=1", ud2)
	}
}
