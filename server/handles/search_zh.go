package handles

import (
	"strings"
	"sync"
	"unicode"

	"github.com/longbridgeapp/opencc"
	"github.com/mozillazg/go-pinyin"
)

type zhTitleIndex struct {
	simp        string
	trad        string
	simpCompact string
	tradCompact string
	fullPinyin  string
	initials    string
}

var (
	zhOpenCCOnce sync.Once
	zhT2S        *opencc.OpenCC
	zhS2T        *opencc.OpenCC

	zhSearchCache sync.Map
)

func initZhOpenCC() {
	zhOpenCCOnce.Do(func() {
		zhT2S, _ = opencc.New("t2s")
		zhS2T, _ = opencc.New("s2t")
	})
}

func zhToSimplified(s string) string {
	if s == "" || !hasHan(s) {
		return s
	}
	initZhOpenCC()
	if zhT2S == nil {
		return s
	}
	if out, err := zhT2S.Convert(s); err == nil && out != "" {
		return out
	}
	return s
}

func zhToTraditional(s string) string {
	if s == "" || !hasHan(s) {
		return s
	}
	initZhOpenCC()
	if zhS2T == nil {
		return s
	}
	if out, err := zhS2T.Convert(s); err == nil && out != "" {
		return out
	}
	return s
}

func stripSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func getZhTitleIndex(title string) *zhTitleIndex {
	if cached, ok := zhSearchCache.Load(title); ok {
		return cached.(*zhTitleIndex)
	}

	simpRaw := zhToSimplified(title)
	tradRaw := zhToTraditional(title)
	simp := strings.ToLower(simpRaw)
	trad := strings.ToLower(tradRaw)

	idx := &zhTitleIndex{
		simp:        simp,
		trad:        trad,
		simpCompact: stripSpaces(simp),
		tradCompact: stripSpaces(trad),
	}

	if hasHan(title) {
		full, initStr := buildZhPinyinIndex(simpRaw)
		idx.fullPinyin = full
		idx.initials = initStr
	}

	zhSearchCache.Store(title, idx)
	return idx
}

// buildZhPinyinIndex 基于简体化后的标题生成全拼与首字母索引串（全小写、去空白标点，保留字母数字）
func buildZhPinyinIndex(simplified string) (full string, initials string) {
	runes := []rune(simplified)
	var fullBuf, initBuf strings.Builder
	fullBuf.Grow(len(runes) * 4)
	initBuf.Grow(len(runes))

	for i := 0; i < len(runes); {
		if syls, n := matchPinyinPhrase(runes, i); n > 0 {
			for _, syl := range syls {
				ls := strings.ToLower(syl)
				if ls != "" {
					fullBuf.WriteString(ls)
					initBuf.WriteByte(ls[0])
				}
			}
			i += n
			continue
		}

		r := runes[i]
		i++
		if unicode.Is(unicode.Han, r) {
			py := pinyin.LazyPinyin(string(r), pinyinNormalArgs)
			if len(py) > 0 && py[0] != "" {
				syl := strings.ToLower(py[0])
				fullBuf.WriteString(syl)
				initBuf.WriteByte(syl[0])
			} else {
				lr := unicode.ToLower(r)
				fullBuf.WriteRune(lr)
				initBuf.WriteRune(lr)
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			lr := unicode.ToLower(r)
			fullBuf.WriteRune(lr)
			initBuf.WriteRune(lr)
		}
	}
	return fullBuf.String(), initBuf.String()
}

// zhSearchMatch 按以下顺序匹配中文/拼音搜索词：
//  1. 原始小写子串（保持既有行为优先）
//  2. 繁简双向归一化（繁体 ⇄ 繁體）
//  3. 全拼（liulangdiqiu）与拼音首字母（lldq），查询词先剥离空白（如 "liu lang"）
func zhSearchMatch(title, query string) bool {
	lQuery := strings.ToLower(strings.TrimSpace(query))
	if lQuery == "" {
		return true
	}
	if title == "" {
		return false
	}

	// 1. 原生小写子串匹配优先
	lTitle := strings.ToLower(title)
	if strings.Contains(lTitle, lQuery) {
		return true
	}

	compactQuery := stripSpaces(lQuery)
	if compactQuery == "" {
		return true
	}
	if compactQuery != lQuery && strings.Contains(stripSpaces(lTitle), compactQuery) {
		return true
	}

	titleHasHan := hasHan(title)
	queryHasHan := hasHan(compactQuery)
	if !titleHasHan && !queryHasHan {
		return false
	}

	idx := getZhTitleIndex(title)

	// 2. 繁简双向归一化匹配
	if queryHasHan {
		qSimp := strings.ToLower(zhToSimplified(lQuery))
		qTrad := strings.ToLower(zhToTraditional(lQuery))
		if strings.Contains(idx.simp, qSimp) || strings.Contains(idx.trad, qTrad) {
			return true
		}
		if compactQuery != lQuery {
			if strings.Contains(idx.simpCompact, stripSpaces(qSimp)) ||
				strings.Contains(idx.tradCompact, stripSpaces(qTrad)) {
				return true
			}
		}
	}

	// 3 & 4. 全拼与拼音首字母匹配（查询词已剥离空格）
	if titleHasHan {
		if idx.fullPinyin != "" && strings.Contains(idx.fullPinyin, compactQuery) {
			return true
		}
		if idx.initials != "" && strings.Contains(idx.initials, compactQuery) {
			return true
		}
	}

	return false
}

// embyItemMatchesSearch 对 Emby 条目执行搜索词过滤：
// Movie 与 Series 启用中文增强搜索（繁简+全拼+首字母），其余类型保持原有大小写不敏感子串匹配。
func embyItemMatchesSearch(it *embyItem, rawTerm string) bool {
	term := strings.TrimSpace(rawTerm)
	if term == "" {
		return true
	}
	if it == nil {
		return false
	}
	if it.Type == "Movie" || it.Type == "Series" {
		return zhSearchMatch(it.Name, term)
	}
	return strings.Contains(strings.ToLower(it.Name), strings.ToLower(term))
}
