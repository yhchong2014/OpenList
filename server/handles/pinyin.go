package handles

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/mozillazg/go-pinyin"
)

// pinyinPhrases 维护常见影视/音乐标题中的多音字词组读音（全拼小写，每字一节）。
// 同时供首字母排序（pinyinSortName）和中文拼音搜索（search_zh.go）复用；
// 新增词组只需添加一行。匹配时按最长词组优先（如「重庆森林」优先于「重庆」）。
var pinyinPhrases = map[string][]string{
	// 重 chong / zhong
	"重庆森林": {"chong", "qing", "sen", "lin"},
	"重庆":   {"chong", "qing"},
	"重新":   {"chong", "xin"},
	"重复":   {"chong", "fu"},
	"重逢":   {"chong", "feng"},
	"重生":   {"chong", "sheng"},
	"重返":   {"chong", "fan"},
	"重量":   {"zhong", "liang"},
	"重要":   {"zhong", "yao"},

	// 长 chang / zhang
	"长城":  {"chang", "cheng"},
	"长安":  {"chang", "an"},
	"长空":  {"chang", "kong"},
	"长江":  {"chang", "jiang"},
	"长沙":  {"chang", "sha"},
	"长春":  {"chang", "chun"},
	"长津湖": {"chang", "jin", "hu"},
	"大长今": {"da", "chang", "jin"},
	"漫长":  {"man", "chang"},
	"长征":  {"chang", "zheng"},
	"长大":  {"zhang", "da"},

	// 行 hang / xing
	"银行": {"yin", "hang"},
	"行长": {"hang", "zhang"},
	"行为": {"xing", "wei"},
	"行业": {"hang", "ye"},
	"同行": {"tong", "hang"},

	// 乐 yue / le
	"音乐": {"yin", "yue"},
	"乐器": {"yue", "qi"},
	"乐谱": {"yue", "pu"},
	"乐章": {"yue", "zhang"},
	"乐队": {"yue", "dui"},
	"快乐": {"kuai", "le"},

	// 参 / 曾 / 单 / 藏 / 调 / 传
	"人参":   {"ren", "shen"},
	"曾经":   {"ceng", "jing"},
	"姓曾":   {"xing", "zeng"},
	"单于":   {"chan", "yu"},
	"姓单":   {"xing", "shan"},
	"西藏":   {"xi", "zang"},
	"宝藏":   {"bao", "zang"},
	"调音师":  {"tiao", "yin", "shi"},
	"水浒传":  {"shui", "hu", "zhuan"},
	"英雄传":  {"ying", "xiong", "zhuan"},
	"武林外传": {"wu", "lin", "wai", "zhuan"},
	"甄嬛传":  {"zhen", "huan", "zhuan"},
	"如懿传":  {"ru", "yi", "zhuan"},
	"芈月传":  {"mi", "yue", "zhuan"},
	"外传":   {"wai", "zhuan"},
	"正传":   {"zheng", "zhuan"},
	"前传":   {"qian", "zhuan"},
	"后传":   {"hou", "zhuan"},
	"自传":   {"zi", "zhuan"},
	"左传":   {"zuo", "zhuan"},
}

var (
	pinyinMaxPhraseLen int
	pinyinSortCache    sync.Map
	pinyinNormalArgs   pinyin.Args
	pinyinInitialArgs  pinyin.Args
)

func init() {
	for phrase := range pinyinPhrases {
		if n := utf8.RuneCountInString(phrase); n > pinyinMaxPhraseLen {
			pinyinMaxPhraseLen = n
		}
	}
	pinyinNormalArgs = pinyin.NewArgs()
	pinyinInitialArgs = pinyin.NewArgs()
	pinyinInitialArgs.Style = pinyin.FirstLetter
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// matchPinyinPhrase 在 runes[pos:] 处按最长优先匹配多音字词组
func matchPinyinPhrase(runes []rune, pos int) ([]string, int) {
	rem := len(runes) - pos
	maxLen := pinyinMaxPhraseLen
	if rem < maxLen {
		maxLen = rem
	}
	for l := maxLen; l >= 2; l-- {
		if syls, ok := pinyinPhrases[string(runes[pos:pos+l])]; ok {
			return syls, l
		}
	}
	return nil, 0
}

// pinyinSortName 生成供 Emby SortName / ForcedSortName 使用的拼音首字母排序键：
//   - 不含汉字（纯英文/数字/符号）时原样返回且不入缓存；
//   - 含汉字时，仅将汉字转换为大写拼音首字母，非汉字原样保留（拉丁字母统一大写，
//     使「Dune 沙丘」->「DUNE SQ」归入 D 组，「流浪地球2」->「LLDQ2」不丢尾号）。
func pinyinSortName(name string) string {
	if !hasHan(name) {
		return name
	}
	if cached, ok := pinyinSortCache.Load(name); ok {
		return cached.(string)
	}

	normalized := zhToSimplified(name)
	runes := []rune(normalized)
	var b strings.Builder
	b.Grow(len(runes))

	for i := 0; i < len(runes); {
		if syls, n := matchPinyinPhrase(runes, i); n > 0 {
			for _, syl := range syls {
				if syl != "" {
					b.WriteByte(asciiUpper(syl[0]))
				}
			}
			i += n
			continue
		}

		r := runes[i]
		i++
		if unicode.Is(unicode.Han, r) {
			py := pinyin.LazyPinyin(string(r), pinyinInitialArgs)
			if len(py) > 0 && py[0] != "" {
				b.WriteByte(asciiUpper(py[0][0]))
			} else {
				// 字典未收录的生僻汉字原样保留，绝不静默丢弃
				b.WriteRune(r)
			}
			continue
		}
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r - ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}

	out := b.String()
	pinyinSortCache.Store(name, out)
	return out
}

func asciiUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}
