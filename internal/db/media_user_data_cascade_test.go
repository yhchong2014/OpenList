package db

import (
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestMediaDeleteCascadesMediaUserData(t *testing.T) {
	db = openTestDB(t, "ol_")
	if err := db.AutoMigrate(&model.User{}, &model.MediaItem{}, &model.MediaAnnotation{}, &model.MediaUserData{}); err != nil {
		t.Fatal(err)
	}

	items := []*model.MediaItem{
		{MediaType: model.MediaTypeVideo, ScanPathID: 1, FolderPath: "/m", FileName: "a.mkv"},
		{MediaType: model.MediaTypeVideo, ScanPathID: 1, FolderPath: "/m", FileName: "b.mkv"},
		{MediaType: model.MediaTypeVideo, ScanPathID: 2, FolderPath: "/m", FileName: "c.mkv"},
		{MediaType: model.MediaTypeMusic, ScanPathID: 3, FolderPath: "/mu", FileName: "d.flac"},
		{MediaType: model.MediaTypeVideo, ScanPathID: 4, FolderPath: "/keep", FileName: "e.mkv"},
	}
	for _, it := range items {
		if err := db.Create(it).Error; err != nil {
			t.Fatal(err)
		}
		if err := SaveMediaUserData(&model.MediaUserData{
			UserID: 1, ItemID: it.ID, EpisodeIndex: 0, Played: true, PlayCount: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 1. DeleteMediaItem
	if err := DeleteMediaItem(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(1, items[0].ID, 0); err == nil {
		t.Fatal("DeleteMediaItem left orphaned MediaUserData")
	}

	// 2. DeleteMediaItemByPath
	if _, err := DeleteMediaItemByPath("/m", "b.mkv"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(1, items[1].ID, 0); err == nil {
		t.Fatal("DeleteMediaItemByPath left orphaned MediaUserData")
	}

	// 3. ClearMediaItemsByScanPath
	if err := ClearMediaItemsByScanPath(2); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(1, items[2].ID, 0); err == nil {
		t.Fatal("ClearMediaItemsByScanPath left orphaned MediaUserData")
	}

	// 4. ClearMediaItems(music)
	if err := ClearMediaItems(model.MediaTypeMusic); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(1, items[3].ID, 0); err == nil {
		t.Fatal("ClearMediaItems left orphaned MediaUserData")
	}

	// items[4] 仍应完好保留，再用 DeleteMediaItemsByIDs 清理
	if _, err := GetMediaUserData(1, items[4].ID, 0); err != nil {
		t.Fatalf("unrelated MediaUserData should still exist: %v", err)
	}
	if err := DeleteMediaItemsByIDs([]uint{items[4].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(1, items[4].ID, 0); err == nil {
		t.Fatal("DeleteMediaItemsByIDs left orphaned MediaUserData")
	}

	// 5. DeleteUserById 清理该用户的播放状态
	u := &model.User{Username: "tempuser"}
	if err := CreateUser(u); err != nil {
		t.Fatal(err)
	}
	if err := SaveMediaUserData(&model.MediaUserData{UserID: u.ID, ItemID: 999, EpisodeIndex: 0, Played: true}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUserById(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMediaUserData(u.ID, 999, 0); err == nil {
		t.Fatal("DeleteUserById left orphaned MediaUserData")
	}
}

func TestRescanPreservesDurationWithoutScrape(t *testing.T) {
	db = openTestDB(t, "ol_")
	if err := db.AutoMigrate(&model.MediaItem{}); err != nil {
		t.Fatal(err)
	}

	first := &model.MediaItem{
		MediaType:  model.MediaTypeVideo,
		FolderPath: "/movies",
		FileName:   "local.mkv",
		Duration:   5400,
	}
	if err := CreateOrUpdateMediaItem(first); err != nil {
		t.Fatal(err)
	}

	fixedUpdated := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	db.Model(first).UpdateColumn("updated_at", fixedUpdated)
	if err := UpdateMediaItemDuration(first.ID, 9999); err != nil {
		t.Fatal(err)
	}
	afterNoop, _ := GetMediaItemByID(first.ID)
	if afterNoop.Duration != 5400 || !afterNoop.UpdatedAt.Equal(fixedUpdated) {
		t.Fatalf("UpdateMediaItemDuration overwrote non-zero duration or updated_at: %+v", afterNoop)
	}

	rescan := &model.MediaItem{
		MediaType:  model.MediaTypeVideo,
		FolderPath: "/movies",
		FileName:   "local.mkv",
		Duration:   0,
	}
	if err := CreateOrUpdateMediaItem(rescan); err != nil {
		t.Fatal(err)
	}
	got, err := GetMediaItemByID(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Duration != 5400 {
		t.Fatalf("rescan wiped Duration: got %d, want 5400", got.Duration)
	}
}
