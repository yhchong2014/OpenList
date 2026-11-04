package handles

import (
	"fmt"
	"net/url"
	stdpath "path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/media/scraper"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// embyMovieVersion 表示一部电影下的单个可播放版本（如 1080p / 4K / 导演剪辑版）。
type embyMovieVersion struct {
	SourceID  string
	Item      *model.MediaItem
	Episode   *model.EpisodeInfo
	RawPath   string
	FileName  string
	Container string
	Size      int64
	Duration  int
	Label     string
}

// embyMovieGroup 聚合同一部电影的多个版本（相同 TMDB/IMDb ID、同名多版本后缀或电影多版本目录）。
type embyMovieGroup struct {
	Key      string
	Primary  *model.MediaItem
	Items    []model.MediaItem
	Versions []embyMovieVersion
}

// isMultiVersionMovieFolder 判断未显式标记 VideoType 的文件夹条目是否为「多版本电影目录」：
// 若文件夹内包含 >=2 个视频文件，不含任何季/集特征，且所有文件名均可按文件夹名拆出多版本标签
// （如 /Movies/Avatar (2009)/Avatar (2009) - 1080p.mkv + Avatar (2009) - 4K.mkv），
// 则按多版本电影而非剧集处理。
func isMultiVersionMovieFolder(it *model.MediaItem) bool {
	if it == nil || !it.IsFolder || it.VideoType != "" {
		return false
	}
	eps := parseEpisodeList(it)
	if len(eps) < 2 {
		return false
	}
	for _, ep := range eps {
		if ep.Season > 0 || seasonFromText(ep.FileName) > 0 || seasonFromText(ep.Title) > 0 {
			return false
		}
		if _, ver := scraper.SplitMovieVersion(ep.FileName, it.FileName); ver == "" {
			return false
		}
	}
	return true
}

func isUneditedMovieScrapedName(it *model.MediaItem) bool {
	if it == nil || it.ScrapedName == "" {
		return true
	}
	defaultStem := strings.TrimSuffix(it.FileName, stdpath.Ext(it.FileName))
	if it.ScrapedName == defaultStem {
		return true
	}
	if it.ScrapedName == scraper.StripProviderTags(defaultStem) {
		return true
	}
	base, _ := scraper.SplitMovieVersion(it.FileName, it.FolderPath)
	return base != "" && it.ScrapedName == base
}

func movieGroupKey(it *model.MediaItem) string {
	if ext := strings.ToLower(strings.TrimSpace(it.ExternalID)); ext != "" {
		return "ext:" + ext
	}
	ids := scraper.ExtractProviderIDsFromItem(it)
	if ids.TmdbID != "" {
		return "ext:tmdb:" + ids.TmdbID
	}
	if ids.ImdbID != "" {
		return "ext:imdb:" + ids.ImdbID
	}

	folder := strings.ToLower(strings.TrimSuffix(it.FolderPath, "/"))
	base, _ := scraper.SplitMovieVersion(it.FileName, it.FolderPath)
	if base == "" {
		base = strings.TrimSuffix(it.FileName, stdpath.Ext(it.FileName))
	}
	if !isUneditedMovieScrapedName(it) {
		return fmt.Sprintf("manual:%s|%s|%d", folder, strings.ToLower(it.ScrapedName), it.ID)
	}
	return "base:" + folder + "|" + strings.ToLower(base)
}

func buildVersionsForMovieItem(it *model.MediaItem) []embyMovieVersion {
	if it.IsFolder {
		eps := parseEpisodeList(it)
		if len(eps) > 0 {
			out := make([]embyMovieVersion, 0, len(eps))
			for idx := range eps {
				ep := eps[idx]
				raw := MediaEpisodeFilePath(it, &ep)
				out = append(out, embyMovieVersion{
					SourceID:  fmt.Sprintf("mvf-%d-%d", it.ID, idx),
					Item:      it,
					Episode:   &ep,
					RawPath:   raw,
					FileName:  stdpath.Base(ep.FileName),
					Container: embyExt(ep.FileName),
					Size:      it.FileSize,
					Duration:  it.Duration,
				})
			}
			return out
		}
	}
	return []embyMovieVersion{{
		SourceID:  fmt.Sprintf("mv-%d", it.ID),
		Item:      it,
		RawPath:   MediaItemFilePath(it),
		FileName:  it.FileName,
		Container: embyExt(it.FileName),
		Size:      it.FileSize,
		Duration:  it.Duration,
	}}
}

func commonStemPrefix(stems []string) string {
	if len(stems) < 2 {
		return ""
	}
	prefix := stems[0]
	for _, s := range stems[1:] {
		maxLen := len(prefix)
		if len(s) < maxLen {
			maxLen = len(s)
		}
		i := 0
		for i < maxLen && strings.EqualFold(prefix[i:i+1], s[i:i+1]) {
			i++
		}
		prefix = prefix[:i]
		if prefix == "" {
			return ""
		}
	}
	// 回退到分隔符边界，避免把 "1080p" 与 "1080i" 截成 "p" / "i"
	lastSep := strings.LastIndexAny(prefix, " .-_([")
	if lastSep <= 0 {
		return ""
	}
	return prefix[:lastSep+1]
}

func assignMovieVersionLabels(versions []embyMovieVersion) {
	if len(versions) == 0 {
		return
	}
	if len(versions) == 1 {
		versions[0].Label = versions[0].FileName
		return
	}

	stems := make([]string, len(versions))
	for i, v := range versions {
		stems[i] = strings.TrimSuffix(v.FileName, stdpath.Ext(v.FileName))
		parent := v.Item.FolderPath
		if v.Item.IsFolder {
			parent = v.Item.FileName
		}
		if _, ver := scraper.SplitMovieVersion(v.FileName, parent); ver != "" {
			versions[i].Label = ver
		}
	}

	shared := commonStemPrefix(stems)
	for i := range versions {
		if versions[i].Label != "" {
			continue
		}
		stem := scraper.StripProviderTags(stems[i])
		if shared != "" && len(stem) > len(shared) && strings.EqualFold(stem[:len(shared)], shared) {
			trimmed := strings.TrimLeft(stem[len(shared):], " .-_")
			if trimmed != "" {
				versions[i].Label = trimmed
				continue
			}
		}
		if stem != "" {
			versions[i].Label = stem
		} else {
			versions[i].Label = versions[i].FileName
		}
	}
}

func (l *embyLibrary) movieGroups() []*embyMovieGroup {
	if l.movieGroupList != nil {
		return l.movieGroupList
	}
	byKey := map[string]*embyMovieGroup{}
	var order []string
	for i := range l.videos {
		it := &l.videos[i]
		if isEmbySeriesItem(it) {
			continue
		}
		key := movieGroupKey(it)
		g, ok := byKey[key]
		if !ok {
			g = &embyMovieGroup{Key: key}
			byKey[key] = g
			order = append(order, key)
		}
		g.Items = append(g.Items, *it)
	}

	groups := make([]*embyMovieGroup, 0, len(order))
	for _, k := range order {
		g := byKey[k]
		// 按 ID 升序稳定选取 Primary，确保后续新增第二版本时原有 Movie Id 不漂移
		sort.SliceStable(g.Items, func(i, j int) bool {
			return g.Items[i].ID < g.Items[j].ID
		})
		g.Primary = &g.Items[0]
		for i := range g.Items {
			g.Versions = append(g.Versions, buildVersionsForMovieItem(&g.Items[i])...)
		}
		assignMovieVersionLabels(g.Versions)
		groups = append(groups, g)
	}
	l.movieGroupList = groups
	return groups
}

func (l *embyLibrary) movieGroupByItemID(itemID uint) *embyMovieGroup {
	for _, g := range l.movieGroups() {
		for i := range g.Items {
			if g.Items[i].ID == itemID {
				return g
			}
		}
	}
	return nil
}

func (l *embyLibrary) movieGroupByOrigID(orig string) *embyMovieGroup {
	switch {
	case strings.HasPrefix(orig, "mv-"):
		n, err := strconv.ParseUint(strings.TrimPrefix(orig, "mv-"), 10, 64)
		if err != nil {
			return nil
		}
		return l.movieGroupByItemID(uint(n))
	case strings.HasPrefix(orig, "mvf-"):
		parts := strings.Split(orig, "-")
		if len(parts) != 3 {
			return nil
		}
		n, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil
		}
		return l.movieGroupByItemID(uint(n))
	}
	return nil
}

func (g *embyMovieGroup) displayName() string {
	for i := range g.Items {
		it := &g.Items[i]
		if !isUneditedMovieScrapedName(it) && it.ScrapedName != "" {
			return it.ScrapedName
		}
		if (it.ScrapedAt != nil || it.ExternalID != "") && it.ScrapedName != "" {
			return scraper.StripProviderTags(it.ScrapedName)
		}
	}
	primary := g.Primary
	base, _ := scraper.SplitMovieVersion(primary.FileName, primary.FolderPath)
	if base != "" {
		return base
	}
	if primary.ScrapedName != "" {
		return scraper.StripProviderTags(primary.ScrapedName)
	}
	return strings.TrimSuffix(primary.FileName, stdpath.Ext(primary.FileName))
}

func (g *embyMovieGroup) coverItem() *model.MediaItem {
	for i := range g.Items {
		if g.Items[i].Cover != "" {
			return &g.Items[i]
		}
	}
	return g.Primary
}

func (g *embyMovieGroup) metadataItem() *model.MediaItem {
	for i := range g.Items {
		if g.Items[i].ScrapedAt != nil || g.Items[i].Plot != "" || g.Items[i].ReleaseDate != "" || g.Items[i].ExternalID != "" {
			return &g.Items[i]
		}
	}
	return g.Primary
}

func (g *embyMovieGroup) durationSeconds() int {
	for i := range g.Items {
		if g.Items[i].Duration > 0 {
			return g.Items[i].Duration
		}
	}
	return 0
}

func (g *embyMovieGroup) latestCreatedAt() time.Time {
	latest := g.Primary.CreatedAt
	for i := range g.Items {
		if g.Items[i].CreatedAt.After(latest) {
			latest = g.Items[i].CreatedAt
		}
	}
	return latest
}

func (l *embyLibrary) movieGroupUserData(g *embyMovieGroup) *embyUserData {
	if l == nil || l.user == nil || g == nil {
		return &embyUserData{}
	}
	out := &embyUserData{}
	var latestResumeTime time.Time
	for i := range g.Items {
		d, ok := l.userData[[2]uint{g.Items[i].ID, 0}]
		if !ok || d == nil {
			continue
		}
		if d.Played {
			out.Played = true
		}
		if d.IsFavorite {
			out.IsFavorite = true
		}
		out.PlayCount += d.PlayCount
		if d.PositionTicks > 0 {
			var ts time.Time
			if d.LastPlayedAt != nil {
				ts = *d.LastPlayedAt
			}
			if out.PlaybackPositionTicks == 0 || ts.After(latestResumeTime) {
				out.PlaybackPositionTicks = d.PositionTicks
				latestResumeTime = ts
			}
		}
	}
	if out.Played {
		out.PlaybackPositionTicks = 0
	}
	return out
}

func (l *embyLibrary) buildMovieMediaSources(g *embyMovieGroup, movieGuid, apiBase, token string) []embyMediaSource {
	if g == nil || len(g.Versions) == 0 {
		return []embyMediaSource{}
	}
	groupDur := g.durationSeconds()
	out := make([]embyMediaSource, 0, len(g.Versions))
	for _, ver := range g.Versions {
		sourceGuid := l.guid(ver.SourceID)
		q := url.Values{}
		q.Set("Static", "true")
		q.Set("MediaSourceId", sourceGuid)
		if token != "" {
			q.Set("api_key", token)
		}
		streamPath := fmt.Sprintf("/@emby/Videos/%s/stream.%s?%s", sourceGuid, ver.Container, q.Encode())
		if ver.Container == "" {
			streamPath = fmt.Sprintf("/@emby/Videos/%s/stream?%s", sourceGuid, q.Encode())
		}
		fullURL := streamPath
		if apiBase != "" {
			fullURL = strings.TrimSuffix(apiBase, "/") + streamPath
		}
		dur := ver.Duration
		if dur <= 0 {
			dur = groupDur
		}
		out = append(out, embyMediaSource{
			Protocol:                   "Http",
			Id:                         sourceGuid,
			ItemId:                     movieGuid,
			Path:                       fullURL,
			Type:                       "Default",
			Container:                  ver.Container,
			Name:                       ver.Label,
			Size:                       ver.Size,
			ETag:                       embyHash(fmt.Sprintf("%s|%d", ver.RawPath, ver.Size)),
			RunTimeTicks:               embyTicks(dur),
			SupportsDirectPlay:         true,
			SupportsDirectStream:       true,
			SupportsTranscoding:        false,
			SupportsProbing:            false,
			MediaStreams:               []embyMediaStream{{Type: "Video", Index: 0, IsDefault: true, DisplayTitle: ver.Label}},
			MediaAttachments:           []any{},
			Formats:                    []string{},
			RequiredHttpHeaders:        map[string]string{},
			DirectStreamUrl:            fullURL,
			AddApiKeyToDirectStreamUrl: false,
		})
	}
	return out
}

func (l *embyLibrary) movieGroupItem(g *embyMovieGroup) *embyItem {
	meta := g.metadataItem()
	coverIt := g.coverItem()
	year := 0
	if len(meta.ReleaseDate) >= 4 {
		year, _ = strconv.Atoi(meta.ReleaseDate[:4])
	}
	name := g.displayName()
	sortName := pinyinSortName(name)
	movieGuid := l.guid(fmt.Sprintf("mv-%d", g.Primary.ID))
	sources := l.buildMovieMediaSources(g, movieGuid, "", "")
	primaryPath := MediaItemFilePath(g.Primary)
	primaryContainer := embyExt(g.Primary.FileName)
	if len(g.Versions) > 0 {
		primaryPath = g.Versions[0].RawPath
		primaryContainer = g.Versions[0].Container
	}

	e := &embyItem{
		Name:             name,
		SortName:         sortName,
		ForcedSortName:   sortName,
		ServerId:         embyServerID(),
		Id:               movieGuid,
		ParentId:         l.guid("lib-movie"),
		Type:             "Movie",
		MediaType:        "Video",
		IsFolder:         false,
		Path:             primaryPath,
		Container:        primaryContainer,
		RunTimeTicks:     embyTicks(g.durationSeconds()),
		ProductionYear:   year,
		Overview:         meta.Plot,
		CommunityRating:  meta.Rating,
		PremiereDate:     meta.ReleaseDate,
		DateCreated:      embyTimeStr(g.latestCreatedAt()),
		UserData:         l.movieGroupUserData(g),
		ImageTags:        map[string]string{},
		ProviderIds:      map[string]string{},
		MediaSources:     sources,
		MediaSourceCount: len(sources),
		LocationType:     "FileSystem",
	}
	if tag := embyImageTag(coverIt); tag != "" {
		e.ImageTags["Primary"] = tag
	}
	if meta.ExternalID != "" {
		e.ProviderIds["Tmdb"] = strings.TrimPrefix(meta.ExternalID, "tmdb:")
	} else if ids := scraper.ExtractProviderIDsFromItem(meta); ids.TmdbID != "" {
		e.ProviderIds["Tmdb"] = ids.TmdbID
	}
	return e
}
