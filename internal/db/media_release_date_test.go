package db

import (
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestParseMediaReleaseDate(t *testing.T) {
	cases := []struct {
		raw    string
		want   time.Time
		wantOK bool
	}{
		{"2019-02-05", time.Date(2019, 2, 5, 0, 0, 0, 0, time.UTC), true},
		{"2021/05/12", time.Date(2021, 5, 12, 0, 0, 0, 0, time.UTC), true},
		{"2021.05.12", time.Date(2021, 5, 12, 0, 0, 0, 0, time.UTC), true},
		{"2021年2月11日", time.Date(2021, 2, 11, 0, 0, 0, 0, time.UTC), true},
		{"2021-05", time.Date(2021, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"2021年5月", time.Date(2021, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"1998-05-00", time.Date(1998, 5, 1, 0, 0, 0, 0, time.UTC), true},
		{"2021-00-00", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2021", time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"May 2019", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"2021-02-30", time.Date(2021, 2, 28, 0, 0, 0, 0, time.UTC), true},
		{"2021:06:15 08:30:00", time.Date(2021, 6, 15, 8, 30, 0, 0, time.UTC), true},
		{"", time.Time{}, false},
		{"unknown", time.Time{}, false},
	}

	for _, tc := range cases {
		got, ok := ParseMediaReleaseDate(tc.raw)
		if ok != tc.wantOK {
			t.Fatalf("ParseMediaReleaseDate(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
		}
		if ok && !got.Equal(tc.want) {
			t.Errorf("ParseMediaReleaseDate(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestSyncAllMediaCreatedAtToReleaseDateAndWriteHooks(t *testing.T) {
	db = openTestDB(t, "ol_")
	resetMediaDateAddedFollowReleaseCache()
	t.Cleanup(func() {
		resetMediaDateAddedFollowReleaseCache()
	})
	if err := db.AutoMigrate(&model.SettingItem{}, &model.MediaItem{}); err != nil {
		t.Fatal(err)
	}

	scanTime := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	fixedUpdated := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	// 开关关闭时入库：保持扫描时间
	SetMediaDateAddedFollowRelease(false)
	m1 := &model.MediaItem{
		CreatedAt:   scanTime,
		UpdatedAt:   fixedUpdated,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/movies",
		FileName:    "wandering_earth.mkv",
		ReleaseDate: "2019-02-05",
	}
	m2 := &model.MediaItem{
		CreatedAt:   scanTime,
		UpdatedAt:   fixedUpdated,
		MediaType:   model.MediaTypeMusic,
		FolderPath:  "/music",
		FileName:    "track.flac",
		ReleaseDate: "2021",
	}
	mUnscraped := &model.MediaItem{
		CreatedAt:  scanTime,
		UpdatedAt:  fixedUpdated,
		MediaType:  model.MediaTypeVideo,
		FolderPath: "/movies",
		FileName:   "unscraped.mkv",
	}
	for _, it := range []*model.MediaItem{m1, m2, mUnscraped} {
		if err := CreateOrUpdateMediaItem(it); err != nil {
			t.Fatal(err)
		}
		db.Model(it).UpdateColumn("updated_at", fixedUpdated)
	}

	// 开启开关并执行全量同步：已有条目的 created_at 应立即更新为发行日期，且不改动 updated_at，未刮削条目保持原样
	SetMediaDateAddedFollowRelease(true)
	synced, err := SyncAllMediaCreatedAtToReleaseDate()
	if err != nil {
		t.Fatalf("SyncAllMediaCreatedAtToReleaseDate error: %v", err)
	}
	if synced != 2 {
		t.Fatalf("synced = %d, want 2", synced)
	}

	got1, _ := GetMediaItemByID(m1.ID)
	if !got1.CreatedAt.UTC().Equal(time.Date(2019, 2, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("m1 CreatedAt = %v, want 2019-02-05 UTC", got1.CreatedAt.UTC())
	}
	if !got1.UpdatedAt.UTC().Equal(fixedUpdated) {
		t.Fatalf("m1 UpdatedAt = %v, want unchanged %v", got1.UpdatedAt.UTC(), fixedUpdated)
	}

	got2, _ := GetMediaItemByID(m2.ID)
	if !got2.CreatedAt.UTC().Equal(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("m2 CreatedAt = %v, want 2021-01-01 UTC", got2.CreatedAt.UTC())
	}

	gotUnscraped, _ := GetMediaItemByID(mUnscraped.ID)
	if !gotUnscraped.CreatedAt.UTC().Equal(scanTime) {
		t.Fatalf("unscraped CreatedAt = %v, want unchanged %v", gotUnscraped.CreatedAt.UTC(), scanTime)
	}

	// 后续刮削/编辑未刮削条目写入 ReleaseDate -> UpdateMediaItem 自动同步 CreatedAt
	gotUnscraped.ReleaseDate = "2024-03-01"
	if err := UpdateMediaItem(gotUnscraped); err != nil {
		t.Fatal(err)
	}
	gotAfterScrape, _ := GetMediaItemByID(mUnscraped.ID)
	if !gotAfterScrape.CreatedAt.UTC().Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("after scrape CreatedAt = %v, want 2024-03-01 UTC", gotAfterScrape.CreatedAt.UTC())
	}
}
