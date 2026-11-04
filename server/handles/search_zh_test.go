package handles

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestZhSearchMatch(t *testing.T) {
	mustMatch := []struct {
		title string
		query string
	}{
		{"流浪地球", "流浪地球"},
		{"流浪地球", "liulang"},
		{"流浪地球", "lldq"},
		{"流浪地球", "流浪"},
		{"流浪地球", "liu lang"},
		{"流浪地球", "LIU LANG DI QIU"},
		{"流浪地球2", "lldq2"},
		{"重庆森林", "cqsl"},
		{"重庆森林", "chong qing"},
		{"重慶森林", "cqsl"},
		{"重慶森林", "重庆"},
		{"繁體", "繁体"},
		{"繁体", "繁體"},
		{"杀手", "殺手"},
		{"殺手", "杀手"},
		{"音乐之声", "yyzs"},
		{"Dune 沙丘", "dune sq"},
		{"Dune 沙丘", "sha qiu"},
		{"2001 A Space Odyssey", "space odyssey"},
		{"流浪地球", ""},
	}
	for _, tc := range mustMatch {
		if !zhSearchMatch(tc.title, tc.query) {
			t.Errorf("zhSearchMatch(%q, %q) = false, want true", tc.title, tc.query)
		}
	}

	mustNotMatch := []struct {
		title string
		query string
	}{
		{"流浪地球", "ldlsw"},
		{"重庆森林", "zqsl"},
		{"流浪地球", "zxcvbn"},
		{"Alien", "lldq"},
		{"", "lldq"},
	}
	for _, tc := range mustNotMatch {
		if zhSearchMatch(tc.title, tc.query) {
			t.Errorf("zhSearchMatch(%q, %q) = true, want false", tc.title, tc.query)
		}
	}
}

func newSearchTestCtx(rawURL string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, rawURL, nil)
	return c
}

func TestFilterEmbyItemsZhSearchMoviesAndSeriesScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	items := []*embyItem{
		{Id: "1", Name: "流浪地球", Type: "Movie", MediaType: "Video"},
		{Id: "2", Name: "长安十二时辰", Type: "Series", IsFolder: true},
		{Id: "3", Name: "流浪歌手", Type: "Audio", MediaType: "Audio"},
	}

	// Movie 拼音首字母命中
	got := filterEmbyItems(items, newSearchTestCtx("/Items?SearchTerm=lldq"))
	if len(got) != 1 || got[0].Name != "流浪地球" {
		t.Fatalf("SearchTerm=lldq got %+v, want [流浪地球]", got)
	}

	// Series 拼音带空格查询命中
	got = filterEmbyItems(items, newSearchTestCtx("/Items?SearchTerm=chang+an"))
	if len(got) != 1 || got[0].Name != "长安十二时辰" {
		t.Fatalf("SearchTerm=chang+an got %+v, want [长安十二时辰]", got)
	}

	// Audio 仅走普通子串，不触发拼音匹配（范围限定在 Movie + Series），但普通子串仍可用
	got = filterEmbyItems(items, newSearchTestCtx("/Items?SearchTerm=llgs"))
	if len(got) != 0 {
		t.Fatalf("Audio item should not match pinyin query llgs, got %+v", got)
	}

	got = filterEmbyItems(items, newSearchTestCtx("/Items?SearchTerm=流浪"))
	if len(got) != 2 {
		t.Fatalf("Plain substring SearchTerm=流浪 should match Movie and Audio, got len=%d", len(got))
	}
}
