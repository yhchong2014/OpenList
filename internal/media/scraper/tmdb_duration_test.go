package scraper

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func initScraperTestDB(t *testing.T) {
	t.Helper()
	conf.Conf = &conf.Config{}
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Init(gdb)
	t.Cleanup(func() {
		db.Close()
	})
}

func TestTMDBScrapePopulatesDuration(t *testing.T) {
	initScraperTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/search/movie"):
			fmt.Fprint(w, `{"results":[{"id":101,"title":"流浪地球","release_date":"2019-02-05","vote_average":7.9,"media_type":"movie"}]}`)
		case r.URL.Path == "/3/movie/101":
			fmt.Fprint(w, `{"id":101,"title":"流浪地球","release_date":"2019-02-05","runtime":125,"vote_average":7.9}`)
		case strings.HasPrefix(r.URL.Path, "/3/search/tv"):
			fmt.Fprint(w, `{"results":[{"id":202,"name":"长安十二时辰","first_air_date":"2019-06-27","vote_average":8.3,"media_type":"tv"}]}`)
		case r.URL.Path == "/3/tv/202":
			fmt.Fprint(w, `{"id":202,"name":"长安十二时辰","first_air_date":"2019-06-27","episode_run_time":[],"last_episode_to_air":{"runtime":48},"vote_average":8.3}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := NewTMDBScraper("test-key", srv.URL)

	movie := &model.MediaItem{
		MediaType: model.MediaTypeVideo,
		FileName:  "流浪地球.2019.mkv",
	}
	if err := s.ScrapeVideo(movie); err != nil {
		t.Fatalf("ScrapeVideo movie error: %v", err)
	}
	if movie.Duration != 125*60 {
		t.Fatalf("movie Duration = %d, want %d", movie.Duration, 125*60)
	}

	tv := &model.MediaItem{
		MediaType: model.MediaTypeVideo,
		FileName:  "长安十二时辰.S01E01.mkv",
	}
	if err := s.ScrapeVideo(tv); err != nil {
		t.Fatalf("ScrapeVideo tv error: %v", err)
	}
	if tv.Duration != 48*60 {
		t.Fatalf("tv Duration = %d, want %d", tv.Duration, 48*60)
	}
}

func TestParseDiscogsDuration(t *testing.T) {
	if got := parseDiscogsDuration("3:45"); got != 225 {
		t.Fatalf("parseDiscogsDuration(3:45) = %d, want 225", got)
	}
	if got := parseDiscogsDuration("1:02:03"); got != 3723 {
		t.Fatalf("parseDiscogsDuration(1:02:03) = %d, want 3723", got)
	}
	if got := parseDiscogsDuration(""); got != 0 {
		t.Fatalf("parseDiscogsDuration('') = %d, want 0", got)
	}
}
