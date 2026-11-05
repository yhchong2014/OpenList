package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestExtractProviderIDs(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantTMDB string
		wantIMDb string
	}{
		{"emby bracket tmdbid", "tom and jerry [tmdbid=1234].mkv", "1234", ""},
		{"jellyfin dash tmdbid", "Tom and Jerry (2021) [tmdbid-587807].mp4", "587807", ""},
		{"plex curly tmdb", "Dune (2021) {tmdb-438631}.mkv", "438631", ""},
		{"imdb bracket", "The Matrix [imdbid=tt0133093].mkv", "", "tt0133093"},
		{"both tags", "Movie {imdb-tt1361336} [tmdbid=587807].mkv", "587807", "tt1361336"},
		{"bare tmdbid", "tom.and.jerry.tmdbid=9876.1080p.mkv", "9876", ""},
		{"no tag", "Spider-Man - Homecoming (2017) 1080p.mkv", "", ""},
	}
	for _, tc := range cases {
		got := ExtractProviderIDs(tc.input)
		if got.TmdbID != tc.wantTMDB || got.ImdbID != tc.wantIMDb {
			t.Errorf("%s: ExtractProviderIDs(%q) = %+v, want tmdb=%q imdb=%q",
				tc.name, tc.input, got, tc.wantTMDB, tc.wantIMDb)
		}
	}

	// 验证文件夹层级继承（内层优先于外层）
	item := &model.MediaItem{
		FolderPath: "/Movies/Tom and Jerry (2021) [tmdbid=587807]",
		FileName:   "Tom and Jerry (2021) - 4K.mkv",
	}
	fromItem := ExtractProviderIDsFromItem(item)
	if fromItem.TmdbID != "587807" || !fromItem.FromTag {
		t.Fatalf("ExtractProviderIDsFromItem = %+v, want TmdbID=587807 FromTag=true", fromItem)
	}
}

func TestSplitMovieVersion(t *testing.T) {
	cases := []struct {
		file     string
		folder   string
		wantBase string
		wantVer  string
	}{
		{"Movies - 1080p.mkv", "/Video", "Movies", "1080p"},
		{"Movies - 4K.mkv", "/Video", "Movies", "4K"},
		{"Tom and Jerry (2021) [tmdbid=587807] - 4K HDR.mkv", "/Video", "Tom and Jerry (2021)", "4K HDR"},
		{"Avatar (2009) - Director's Cut.mkv", "/Video", "Avatar (2009)", "Director's Cut"},
		{"流浪地球 - 4K高码率版.mkv", "/Video", "流浪地球", "4K高码率版"},
		// 专属同名电影文件夹下的自定义版本名应识别
		{"Avatar (2009) - Theatrical.mkv", "/Movies/Avatar (2009)", "Avatar (2009)", "Theatrical"},
		// 正常副标题绝对不能被误拆为多版本
		{"Spider-Man - Homecoming.mkv", "/Movies", "Spider-Man - Homecoming", ""},
		{"Dune - Part 1.mkv", "/Movies", "Dune - Part 1", ""},
		{"Dune - Part 2.mkv", "/Movies", "Dune - Part 2", ""},
		{"Mission Impossible - Dead Reckoning.mkv", "/Movies", "Mission Impossible - Dead Reckoning", ""},
	}
	for _, tc := range cases {
		gotBase, gotVer := SplitMovieVersion(tc.file, tc.folder)
		if gotBase != tc.wantBase || gotVer != tc.wantVer {
			t.Errorf("SplitMovieVersion(%q, %q) = (%q, %q), want (%q, %q)",
				tc.file, tc.folder, gotBase, gotVer, tc.wantBase, tc.wantVer)
		}
	}
}

func TestParseVideoFileNameStripsProviderTagsAndVersion(t *testing.T) {
	p1 := parseVideoFileName("tom and jerry [tmdbid=1234].mkv")
	if strings.Contains(strings.ToLower(p1.EnglishTitle), "tmdbid") || p1.EnglishTitle != "tom and jerry" {
		t.Fatalf("parseVideoFileName tagged = %+v, want EnglishTitle='tom and jerry'", p1)
	}

	p2 := parseVideoFileName("Dune (2021) {tmdb-438631} - 4K.mkv")
	if p2.EnglishTitle != "Dune" || p2.Year != "2021" {
		t.Fatalf("parseVideoFileName curly+version = %+v, want EnglishTitle='Dune' Year='2021'", p2)
	}
}

func TestTMDBScrapeDirectProviderIDLookup(t *testing.T) {
	initScraperTestDB(t)

	var searchCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/search/"):
			atomic.AddInt32(&searchCalls, 1)
			fmt.Fprint(w, `{"results":[]}`)
		case r.URL.Path == "/3/movie/587807":
			fmt.Fprint(w, `{"id":587807,"title":"猫和老鼠","original_title":"Tom & Jerry","release_date":"2021-02-11","runtime":101,"vote_average":6.8}`)
		case r.URL.Path == "/3/movie/27205":
			fmt.Fprint(w, `{"id":27205,"title":"盗梦空间","original_title":"Inception","release_date":"2010-07-15","runtime":148,"vote_average":8.4}`)
		case r.URL.Path == "/3/find/tt0133093":
			fmt.Fprint(w, `{"movie_results":[{"id":603}],"tv_results":[]}`)
		case r.URL.Path == "/3/movie/603":
			fmt.Fprint(w, `{"id":603,"title":"黑客帝国","original_title":"The Matrix","release_date":"1999-03-31","runtime":136,"vote_average":8.7}`)
		case r.URL.Path == "/3/movie/114611":
			fmt.Fprint(w, `{"id":114611,"title":"毫不相干的电影","original_title":"Unrelated Film","release_date":"2012-01-01","runtime":90}`)
		case r.URL.Path == "/3/tv/114611":
			fmt.Fprint(w, `{"id":114611,"name":"猫和老鼠在纽约","original_name":"Tom and Jerry in New York","first_air_date":"2021-07-01","episode_run_time":[21]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := NewTMDBScraper("test-key", srv.URL)

	// 1. 文件名带 [tmdbid=...] 且包含扫描器默认占位 ScrapedName + 历史错刮 ExternalID -> 应直查详情并覆盖修正
	item1 := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/Movies",
		FileName:    "tom and jerry [tmdbid=587807].mkv",
		ScrapedName: "Wrong Old Title",
		ExternalID:  "tmdb:999999",
	}
	if err := s.ScrapeVideo(item1); err != nil {
		t.Fatalf("ScrapeVideo [tmdbid=587807] error: %v", err)
	}
	if item1.ExternalID != "tmdb:587807" || item1.ScrapedName != "猫和老鼠" || item1.VideoType != "movie" {
		t.Fatalf("item1 = %+v, want ExternalID=tmdb:587807 ScrapedName=猫和老鼠 VideoType=movie", item1)
	}

	// 2. 父目录带 {tmdb-...} 且文件名仅为分辨率 "1080p.mkv" -> 应继承父目录标签直查
	item2 := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/Movies/Inception (2010) {tmdb-27205}",
		FileName:    "1080p.mkv",
		ScrapedName: "1080p",
	}
	if err := s.ScrapeVideo(item2); err != nil {
		t.Fatalf("ScrapeVideo parent {tmdb-27205} error: %v", err)
	}
	if item2.ExternalID != "tmdb:27205" || item2.ScrapedName != "盗梦空间" {
		t.Fatalf("item2 = %+v, want ExternalID=tmdb:27205 ScrapedName=盗梦空间", item2)
	}

	// 3. [imdbid=tt...] 通过 /find 反查
	item3 := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/Movies",
		FileName:    "The Matrix [imdbid=tt0133093].mkv",
		ScrapedName: "The Matrix [imdbid=tt0133093]",
	}
	if err := s.ScrapeVideo(item3); err != nil {
		t.Fatalf("ScrapeVideo [imdbid=tt0133093] error: %v", err)
	}
	if item3.ExternalID != "tmdb:603" || item3.ScrapedName != "黑客帝国" {
		t.Fatalf("item3 = %+v, want ExternalID=tmdb:603 ScrapedName=黑客帝国", item3)
	}

	// 4. 未指定 VideoType 时，若 movie/114611 标题毫不相干而 tv/114611 命中片名，应自动选 tv
	item4 := &model.MediaItem{
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/Videos",
		FileName:    "Tom and Jerry in New York [tmdbid=114611].mkv",
		ScrapedName: "Tom and Jerry in New York [tmdbid=114611]",
	}
	if err := s.ScrapeVideo(item4); err != nil {
		t.Fatalf("ScrapeVideo collision [tmdbid=114611] error: %v", err)
	}
	if item4.VideoType != "tv" || item4.ScrapedName != "猫和老鼠在纽约" {
		t.Fatalf("item4 = %+v, want VideoType=tv ScrapedName=猫和老鼠在纽约", item4)
	}

	if got := atomic.LoadInt32(&searchCalls); got != 0 {
		t.Fatalf("expected 0 /search calls when provider IDs resolve directly, got %d", got)
	}
}
