package handles

import (
	"reflect"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestPinyinSortName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 基础中文片名
		{"流浪地球", "LLDQ"},
		{"霸王别姬", "BWBJ"},
		{"杀手", "SS"},
		{"让子弹飞", "RZDF"},
		{"我不是药神", "WBSYS"},
		{"大话西游", "DHXY"},
		{"无间道", "WJD"},
		{"唐人街探案", "TRJTA"},
		{"战狼", "ZL"},
		{"长安十二时辰", "CASESC"},
		{"音乐之声", "YYZS"},

		// 多音字词组（含最长优先匹配）
		{"重庆森林", "CQSL"},
		{"重庆", "CQ"},
		{"长城", "CC"},
		{"长安", "CA"},
		{"长空", "CK"},
		{"长江", "CJ"},
		{"长沙", "CS"},
		{"长春", "CC"},
		{"长大", "ZD"},
		{"银行", "YH"},
		{"行长", "HZ"},
		{"音乐", "YY"},
		{"乐器", "YQ"},
		{"乐谱", "YP"},
		{"乐章", "YZ"},
		{"乐队", "YD"},
		{"快乐", "KL"},
		{"重新", "CX"},
		{"重复", "CF"},
		{"重量", "ZL"},
		{"重要", "ZY"},
		{"行为", "XW"},
		{"行业", "HY"},
		{"同行", "TH"},
		{"人参", "RS"},
		{"曾经", "CJ"},
		{"姓曾", "XZ"},

		// 繁体多音字标题同样能命中词组表
		{"重慶森林", "CQSL"},
		{"長安十二時辰", "CASESC"},

		// 纯 ASCII / 数字 / 空串原样返回
		{"Alien", "Alien"},
		{"Dune", "Dune"},
		{"Interstellar", "Interstellar"},
		{"", ""},
		{"2001 A Space Odyssey", "2001 A Space Odyssey"},

		// 中英/中数混排保留非汉字部分，不丢尾号
		{"Dune 沙丘", "DUNE SQ"},
		{"流浪地球2", "LLDQ2"},
	}

	for _, tc := range cases {
		if got := pinyinSortName(tc.in); got != tc.want {
			t.Errorf("pinyinSortName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPinyinSortNameCacheBehaviour(t *testing.T) {
	// 清空相关键，验证纯 ASCII 不入缓存、中文稳定命中缓存
	asciiInputs := []string{"", "Alien", "Dune", "Interstellar", "2001 A Space Odyssey"}
	for _, s := range asciiInputs {
		pinyinSortCache.Delete(s)
		got := pinyinSortName(s)
		if got != s {
			t.Fatalf("pinyinSortName(%q) = %q, want unchanged %q", s, got, s)
		}
		if _, cached := pinyinSortCache.Load(s); cached {
			t.Fatalf("ASCII input %q must not be inserted into pinyinSortCache", s)
		}
	}

	zh := "重庆森林"
	pinyinSortCache.Delete(zh)
	first := pinyinSortName(zh)
	second := pinyinSortName(zh)
	if first != "CQSL" || second != "CQSL" {
		t.Fatalf("pinyinSortName(%q) = (%q, %q), want CQSL", zh, first, second)
	}
	if v, ok := pinyinSortCache.Load(zh); !ok || v.(string) != "CQSL" {
		t.Fatalf("expected %q to be cached as CQSL, got (%v, %v)", zh, v, ok)
	}
}

func TestSortEmbyItemsMixedLibraryInterleaves(t *testing.T) {
	lib := &embyLibrary{ids: map[string]string{}}
	titles := []string{"战狼", "Interstellar", "流浪地球", "Dune", "霸王别姬", "Alien", "重庆森林"}
	items := make([]*embyItem, 0, len(titles))
	for i, title := range titles {
		it := &model.MediaItem{
			ID:          uint(i + 1),
			MediaType:   model.MediaTypeVideo,
			FileName:    title + ".mkv",
			ScrapedName: title,
		}
		items = append(items, lib.movieItem(it))
	}

	// 只测题设要求的 6 部交错顺序，同时验证含 Interstellar 的完整序列
	six := []*embyItem{items[5], items[0], items[2], items[3], items[4], items[6]}
	sortEmbyItems(six, "SortName", "Ascending")
	var gotSix []string
	for _, it := range six {
		gotSix = append(gotSix, it.Name)
	}
	wantSix := []string{"Alien", "霸王别姬", "重庆森林", "Dune", "流浪地球", "战狼"}
	if !reflect.DeepEqual(gotSix, wantSix) {
		t.Fatalf("mixed library order = %v, want %v", gotSix, wantSix)
	}

	sortEmbyItems(items, "SortName", "Ascending")
	var gotAll []string
	for _, it := range items {
		gotAll = append(gotAll, it.Name)
	}
	wantAll := []string{"Alien", "霸王别姬", "重庆森林", "Dune", "Interstellar", "流浪地球", "战狼"}
	if !reflect.DeepEqual(gotAll, wantAll) {
		t.Fatalf("full mixed library order = %v, want %v", gotAll, wantAll)
	}
}

func TestEmbyDTOConstructorsSetSortAndForcedSortName(t *testing.T) {
	movie := model.MediaItem{
		ID:          1,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/movies",
		FileName:    "重庆森林.mkv",
		ScrapedName: "重庆森林",
	}
	seriesMedia := model.MediaItem{
		ID:          2,
		MediaType:   model.MediaTypeVideo,
		FolderPath:  "/tv",
		FileName:    "长安十二时辰",
		ScrapedName: "长安十二时辰",
		IsFolder:    true,
		VideoType:   "tv",
		Episodes:    `[{"file_name":"S01E02.mkv","index":2,"season":1,"title":"第二集"}]`,
	}
	song := model.MediaItem{
		ID:          3,
		MediaType:   model.MediaTypeMusic,
		FolderPath:  "/music",
		FileName:    "音乐之声.flac",
		ScrapedName: "音乐之声",
		AlbumName:   "霸王别姬原声带",
		AlbumArtist: "群星",
	}

	lib := &embyLibrary{
		videos: []model.MediaItem{movie, seriesMedia},
		music:  []model.MediaItem{song},
		ids:    map[string]string{},
	}

	mvDTO := lib.movieItem(&movie)
	if mvDTO.SortName != "CQSL" || mvDTO.ForcedSortName != "CQSL" {
		t.Fatalf("movieItem SortName=%q ForcedSortName=%q, want CQSL", mvDTO.SortName, mvDTO.ForcedSortName)
	}

	seriesList := lib.seriesList()
	if len(seriesList) != 1 {
		t.Fatalf("seriesList len = %d, want 1", len(seriesList))
	}
	srDTO := lib.seriesItem(seriesList[0])
	if srDTO.SortName != "CASESC" || srDTO.ForcedSortName != "CASESC" {
		t.Fatalf("seriesItem SortName=%q ForcedSortName=%q, want CASESC", srDTO.SortName, srDTO.ForcedSortName)
	}

	// Season 与 Episode 必须保留数字排序键，不能被改成拼音
	snDTO := lib.seasonItem(seriesList[0], 1)
	if snDTO.SortName != "Season 01" || snDTO.ForcedSortName != "" {
		t.Fatalf("seasonItem SortName=%q ForcedSortName=%q, want Season 01 / empty", snDTO.SortName, snDTO.ForcedSortName)
	}
	eps := lib.episodesOf(seriesList[0].Key, 1)
	epDTO := lib.episodeItem(eps[0])
	if epDTO.SortName != "01-002" || epDTO.ForcedSortName != "" {
		t.Fatalf("episodeItem SortName=%q ForcedSortName=%q, want 01-002 / empty", epDTO.SortName, epDTO.ForcedSortName)
	}

	albums := lib.albums()
	alDTO := lib.albumItem(albums[0])
	if alDTO.SortName != "BWBJYSD" || alDTO.ForcedSortName != "BWBJYSD" {
		t.Fatalf("albumItem SortName=%q ForcedSortName=%q, want BWBJYSD", alDTO.SortName, alDTO.ForcedSortName)
	}

	soDTO := lib.songItem(&song)
	if soDTO.SortName != "YYZS" || soDTO.ForcedSortName != "YYZS" {
		t.Fatalf("songItem SortName=%q ForcedSortName=%q, want YYZS", soDTO.SortName, soDTO.ForcedSortName)
	}
}
