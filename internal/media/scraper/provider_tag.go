package scraper

import (
	stdpath "path"
	"regexp"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// ProviderIDs 表示从文件名、目录名或 ExternalID 中解析出的外部元数据 ID。
type ProviderIDs struct {
	TmdbID  string
	ImdbID  string
	FromTag bool // true 表示显式来自文件名/路径命名标签（如 [tmdbid=1234]）
}

var (
	// 支持 Emby / Jellyfin / Plex / Kodi / Radarr / Sonarr 常见命名标签：
	//   [tmdbid=1234]  [tmdbid-1234]  {tmdb-1234}  {tmdbid=1234}  [tmdb=1234]  (tmdbid=1234)
	//   以及非括号裸标签 tmdbid=1234 / tmdbid-1234 / tmdb=1234
	tmdbBracketTagRe = regexp.MustCompile(`(?i)[\[\{\(（【]\s*tmdb(?:id)?\s*[=:\-\s]\s*(\d+)\s*[\]\}\)）】]`)
	tmdbBareTagRe    = regexp.MustCompile(`(?i)(?:^|[\s._\-])tmdb(?:id)?\s*[=:]\s*(\d+)(?:$|[\s._\-])|(?:^|[\s._])tmdb(?:id)?\s*-\s*(\d+)(?:$|[\s._])`)

	imdbBracketTagRe = regexp.MustCompile(`(?i)[\[\{\(（【]\s*imdb(?:id)?\s*[=:\-\s]\s*(tt\d{5,})\s*[\]\}\)）】]`)
	imdbBareTagRe    = regexp.MustCompile(`(?i)(?:^|[\s._\-])imdb(?:id)?\s*[=:]\s*(tt\d{5,})(?:$|[\s._\-])|(?:^|[\s._])imdb(?:id)?\s*-\s*(tt\d{5,})(?:$|[\s._])`)

	// Plex / Radarr 版本标签：{edition-Director's Cut} / [edition=Extended]
	editionTagRe = regexp.MustCompile(`(?i)[\[\{]\s*edition\s*[=:\-\s]\s*([^\]\}]+?)\s*[\]\}]`)

	// 多版本描述词：用于在不依赖父目录名匹配的扁平目录下安全识别 "Movie - 1080p" / "Movie - 4K" / "Movie - Director's Cut"，
	// 同时避免误拆 "Spider-Man - Homecoming" 或 "Dune - Part 1" 这类真实副标题。
	versionPhraseENRe = regexp.MustCompile(`(?i)\b(?:` +
		`director'?s?\s*cut|extended(?:\s*cut|\s*edition)?|final\s*cut|` +
		`theatrical(?:\s*cut|\s*edition)?|special\s*edition|collector'?s?\s*edition|` +
		`ultimate\s*(?:cut|edition)|anniversary\s*edition|unrated(?:\s*cut|\s*edition)?|` +
		`uncut(?:\s*edition)?|remastered|open\s*matte|imax(?:\s*enhanced|\s*edition)?|` +
		`dolby\s*vision|dolby\s*atmos|dts[\s\-]*hd(?:\s*ma)?` +
		`)\b`)

	versionPhraseCNRe = regexp.MustCompile(`(?:` +
		`导演剪辑版?|加长版?|未删减版?|完整版|院线版|公映译制|译制公映|公映版?|正版上译|上译版|京译版|长译版|` +
		`重制版|修复版|收藏版|终极版|特别版|IMAX版?|` +
		`国粤英?[双三多]?[语轨]|国英[双多]?[语轨]|中英[双]?[字幕语]+|双语字幕|特效字幕|内封字幕|外挂字幕|内嵌字幕|` +
		`国语配音|粤语配音|台配版?|港配版?|国配版?|配音版|译制版|原声版|国语|粤语|英语|日语|韩语|双语|中字|` +
		`蓝光原盘|原盘|蓝光版?|高码率版?|高码版?|臻彩版?|杜比视界版?|杜比全景声|杜比版?|帧享版?|高帧率版?|\d{2,3}\s*帧版?` +
		`)`)

	versionTokenRe = regexp.MustCompile(`(?i)\b(?:` +
		`4k|8k|2k|uhd|fhd|hd|sd|2160p|2160i|1440p|1080p|1080i|720p|576p|480p|360p|\d{3,4}[x×]\d{3,4}|` +
		`hdr10\+?|hdr|dovi|dv|sdr|10bit|8bit|12bit|` +
		`hevc|avc|av1|vp9|xvid|divx|x264|x265|h\.?264|h\.?265|` +
		`remux|bluray|blu[\-]?ray|bdrip|bdmv|iso|web[\-]?dl|webrip|web|hdrip|dvdrip|dvd|hdtv|hr[\-]?hdtv|tvrip|` +
		`aac\d*(?:\.\d+)?|ac3|eac3|ddp?\d*(?:\.\d+)?|dd\+|dts[\-x]*|dtshd|flac|mp3|atmos|truehd|ma|` +
		`7\.1|5\.1|2\.0|3d|hsbs|hou|sbs|60fps|120fps|50fps|24fps|hq|proper|repack|internal|v\d+` +
		`)\b`)

	versionSepLeftoverRe = regexp.MustCompile(`[\s\-\._\+/\(\)\[\]\{\}【】（），,&|]+`)
	multiSpaceCollapseRe = regexp.MustCompile(`\s+`)
)

// ExtractProviderIDs 从单个名称片段中提取 TMDB / IMDb 命名标签。
func ExtractProviderIDs(s string) ProviderIDs {
	var out ProviderIDs
	if s == "" {
		return out
	}
	if m := tmdbBracketTagRe.FindStringSubmatch(s); len(m) >= 2 && m[1] != "" {
		out.TmdbID = m[1]
		out.FromTag = true
	} else if m := tmdbBareTagRe.FindStringSubmatch(s); len(m) >= 3 {
		if m[1] != "" {
			out.TmdbID = m[1]
		} else {
			out.TmdbID = m[2]
		}
		if out.TmdbID != "" {
			out.FromTag = true
		}
	}

	if m := imdbBracketTagRe.FindStringSubmatch(s); len(m) >= 2 && m[1] != "" {
		out.ImdbID = strings.ToLower(m[1])
		out.FromTag = true
	} else if m := imdbBareTagRe.FindStringSubmatch(s); len(m) >= 3 {
		id := m[1]
		if id == "" {
			id = m[2]
		}
		if id != "" {
			out.ImdbID = strings.ToLower(id)
			out.FromTag = true
		}
	}
	return out
}

// ExtractProviderIDsFromItem 按「文件名 → 父目录链（由近到远）→ 未刮削条目的手工 ExternalID」优先级提取外部 ID。
func ExtractProviderIDsFromItem(item *model.MediaItem) ProviderIDs {
	if item == nil {
		return ProviderIDs{}
	}
	out := ExtractProviderIDs(item.FileName)
	if out.TmdbID != "" {
		return out
	}

	// 检查父目录链（例如 /Movies/Tom and Jerry [tmdbid=587807]/Tom and Jerry - 1080p.mkv）
	folder := strings.Trim(item.FolderPath, "/")
	if folder != "" {
		segs := strings.Split(folder, "/")
		for i := len(segs) - 1; i >= 0; i-- {
			p := ExtractProviderIDs(segs[i])
			if out.TmdbID == "" && p.TmdbID != "" {
				out.TmdbID = p.TmdbID
				out.FromTag = true
			}
			if out.ImdbID == "" && p.ImdbID != "" {
				out.ImdbID = p.ImdbID
				out.FromTag = true
			}
			if out.TmdbID != "" {
				return out
			}
		}
	}
	if out.ImdbID != "" {
		return out
	}

	// 若尚未刮削但用户在后台手工填写了 ExternalID（如 "tmdb:1234"、"1234"、"tt1234567"），也予以采用
	ext := strings.TrimSpace(item.ExternalID)
	if ext != "" && item.ScrapedAt == nil {
		lower := strings.ToLower(ext)
		switch {
		case strings.HasPrefix(lower, "tmdb:"):
			id := strings.TrimSpace(ext[5:])
			if isAllDigits(id) {
				out.TmdbID = id
			}
		case strings.HasPrefix(lower, "imdb:"):
			id := strings.TrimSpace(lower[5:])
			if strings.HasPrefix(id, "tt") && isAllDigits(id[2:]) {
				out.ImdbID = id
			}
		case strings.HasPrefix(lower, "tt") && isAllDigits(lower[2:]):
			out.ImdbID = lower
		case isAllDigits(ext):
			out.TmdbID = ext
		}
	}
	return out
}

// StripProviderTags 剥离名称中的 [tmdbid=...] / {tmdb-...} / [imdbid=...] / {edition-...} 标签，保留其余文本。
func StripProviderTags(s string) string {
	if s == "" {
		return ""
	}
	s = tmdbBracketTagRe.ReplaceAllString(s, " ")
	s = imdbBracketTagRe.ReplaceAllString(s, " ")
	s = editionTagRe.ReplaceAllString(s, " ")
	s = tmdbBareTagRe.ReplaceAllString(s, " ")
	s = imdbBareTagRe.ReplaceAllString(s, " ")
	s = multiSpaceCollapseRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// IsVersionDescriptor 判断字符串是否完全由分辨率/编码/片源/版本剪辑/音轨字幕等版本描述词组成。
func IsVersionDescriptor(s string) bool {
	s = strings.TrimSpace(StripProviderTags(s))
	if s == "" {
		return false
	}
	work := versionPhraseENRe.ReplaceAllString(s, " ")
	work = versionPhraseCNRe.ReplaceAllString(work, " ")
	work = versionTokenRe.ReplaceAllString(work, " ")
	work = versionSepLeftoverRe.ReplaceAllString(work, "")
	return work == ""
}

func trimVideoExt(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		ext := strings.ToLower(name[idx:])
		// 仅剥离常见媒体扩展名或短扩展名，避免误剥离 "Mr.Bean" 或版本号 ".264"
		switch ext {
		case ".mkv", ".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".ts", ".m2ts", ".iso", ".rmvb", ".mpg", ".mpeg", ".strm":
			return name[:idx]
		}
	}
	return name
}

// SplitMovieVersion 按 Emby 多版本电影命名规范拆分基础片名与版本标签：
//  1. 先剥离视频扩展名与 [tmdbid=...] / {tmdb-...} / [imdbid=...] 标签，并提取 {edition-...}
//  2. 若存在 "<Base> - <Version>"：
//     - 当 parentFolder 与 <Base> 同名（Emby 专用电影目录规则）时，<Version> 可为任意非空标签；
//     - 当处于扁平目录时，仅在 IsVersionDescriptor(<Version>) 为真时拆分，避免误拆正常副标题。
func SplitMovieVersion(fileName, parentFolder string) (baseTitle, versionLabel string) {
	stem := strings.TrimSpace(trimVideoExt(stdpath.Base(fileName)))
	if stem == "" {
		return "", ""
	}

	var editionLabel string
	if m := editionTagRe.FindStringSubmatch(stem); len(m) >= 2 {
		editionLabel = strings.TrimSpace(m[1])
	}
	cleaned := StripProviderTags(stem)
	if cleaned == "" {
		return stem, editionLabel
	}

	cleanParent := ""
	if parentFolder != "" && parentFolder != "/" {
		cleanParent = strings.TrimSpace(StripProviderTags(stdpath.Base(strings.TrimSuffix(parentFolder, "/"))))
	}

	// Emby 官方多版本分隔符：" - "（两侧或至少一侧带空格的连字符，避免拆断 "Spider-Man" / "X-Men"）
	sepPositions := findVersionHyphenSplits(cleaned)
	for _, pos := range sepPositions {
		left := strings.TrimSpace(cleaned[:pos[0]])
		right := strings.TrimSpace(cleaned[pos[1]:])
		if left == "" || right == "" {
			continue
		}
		if cleanParent != "" && strings.EqualFold(left, cleanParent) {
			if editionLabel != "" && !strings.Contains(strings.ToLower(right), strings.ToLower(editionLabel)) {
				right = right + " (" + editionLabel + ")"
			}
			return left, right
		}
		if IsVersionDescriptor(right) {
			if editionLabel != "" && !strings.Contains(strings.ToLower(right), strings.ToLower(editionLabel)) {
				right = right + " (" + editionLabel + ")"
			}
			return left, right
		}
	}

	return cleaned, editionLabel
}

// findVersionHyphenSplits 查找候选多版本分隔符（至少一侧有空白的 '-'，排除单词内部连字符如 Spider-Man / WEB-DL）。
func findVersionHyphenSplits(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); i++ {
		if s[i] != '-' {
			continue
		}
		hasLeftSpace := i > 0 && s[i-1] == ' '
		hasRightSpace := i+1 < len(s) && s[i+1] == ' '
		if !hasLeftSpace && !hasRightSpace {
			continue
		}
		l := i
		for l > 0 && s[l-1] == ' ' {
			l--
		}
		r := i + 1
		for r < len(s) && s[r] == ' ' {
			r++
		}
		out = append(out, [2]int{l, r})
	}
	return out
}
