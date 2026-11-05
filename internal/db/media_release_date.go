package db

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"gorm.io/gorm"
)

// dateAddedFollowReleaseState 缓存 media_date_added_follow_release 开关状态：
// 0 = 未初始化（从 setting_items 表按需加载），1 = 关闭，2 = 开启。
var dateAddedFollowReleaseState atomic.Int32

var (
	releaseFullDateRe  = regexp.MustCompile(`^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})(?:\b|T|\s|$)`)
	releaseYearMonthRe = regexp.MustCompile(`^(\d{4})[-/.](\d{1,2})$`)
	releaseYearOnlyRe  = regexp.MustCompile(`\b(18\d{2}|19\d{2}|20\d{2}|21\d{2})\b`)
)

// SetMediaDateAddedFollowRelease 更新内存中的开关缓存（由 setting hook 或初始化流程调用）。
func SetMediaDateAddedFollowRelease(enabled bool) {
	if enabled {
		dateAddedFollowReleaseState.Store(2)
	} else {
		dateAddedFollowReleaseState.Store(1)
	}
}

func resetMediaDateAddedFollowReleaseCache() {
	dateAddedFollowReleaseState.Store(0)
}

// IsMediaDateAddedFollowReleaseEnabled 判断是否启用了「入库时间跟随上映/发行日期」。
func IsMediaDateAddedFollowReleaseEnabled() bool {
	switch dateAddedFollowReleaseState.Load() {
	case 2:
		return true
	case 1:
		return false
	}
	if db == nil || !db.Migrator().HasTable(&model.SettingItem{}) {
		return false
	}
	item, err := GetSettingItemByKey(conf.MediaDateAddedFollowRelease)
	if err != nil || item == nil {
		dateAddedFollowReleaseState.Store(1)
		return false
	}
	enabled := item.Value == "true" || item.Value == "1"
	SetMediaDateAddedFollowRelease(enabled)
	return enabled
}

func daysInMonth(year, month int) int {
	// 下个月第 0 天即当月最后一天
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// ParseMediaReleaseDate 解析媒体条目的 ReleaseDate 字符串为 UTC 时间：
//   - 完整日期时间（RFC3339 / "YYYY-MM-DD HH:MM:SS" / EXIF "YYYY:MM:DD HH:MM:SS"）保留精确时刻（转为 UTC）
//   - 完整日期（"2021-02-11" / "2021/02/11" / "2021.02.11" / "2021年2月11日"）-> 当天 00:00:00 UTC
//   - 年月（"2021-05" / "2021年5月" / Discogs "1998-05-00"）-> 当月 1 日 00:00:00 UTC
//   - 仅年份（"2021" / "2021年" / Discogs "2021-00-00" / "May 2021"）-> 当年 1 月 1 日 00:00:00 UTC
//   - 空串或无法识别的文本返回 (time.Time{}, false)，调用方保留原始入库时间。
func ParseMediaReleaseDate(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}

	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006:01:02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil && t.Year() >= 1800 && t.Year() <= 2200 {
			return t.UTC(), true
		}
	}

	norm := strings.NewReplacer(
		"年", "-",
		"月", "-",
		"日", "",
		"号", "",
	).Replace(s)
	norm = strings.TrimSpace(strings.TrimSuffix(norm, "-"))

	if m := releaseFullDateRe.FindStringSubmatch(norm); len(m) == 4 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if y >= 1800 && y <= 2200 {
			if mo <= 0 {
				return time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC), true
			}
			if mo >= 1 && mo <= 12 {
				if d <= 0 {
					return time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC), true
				}
				if maxD := daysInMonth(y, mo); d > maxD {
					d = maxD
				}
				return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
			}
		}
	}

	if m := releaseYearMonthRe.FindStringSubmatch(norm); len(m) == 3 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if y >= 1800 && y <= 2200 {
			if mo <= 0 {
				return time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC), true
			}
			if mo >= 1 && mo <= 12 {
				return time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC), true
			}
		}
	}

	if m := releaseYearOnlyRe.FindStringSubmatch(norm); len(m) == 2 {
		y, _ := strconv.Atoi(m[1])
		if y >= 1800 && y <= 2200 {
			return time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC), true
		}
	}

	return time.Time{}, false
}

func applyReleaseDateToCreatedAt(item *model.MediaItem) {
	if item == nil || !IsMediaDateAddedFollowReleaseEnabled() {
		return
	}
	if ts, ok := ParseMediaReleaseDate(item.ReleaseDate); ok {
		item.CreatedAt = ts
	}
}

// SyncAllMediaCreatedAtToReleaseDate 将数据库中所有带有效 release_date 的媒体条目的 created_at
// 原地同步为对应的上映/发行日期（使用 UpdateColumn 仅改写 created_at，不污染 updated_at）。
// 返回实际发生变更的条目数量。
func SyncAllMediaCreatedAtToReleaseDate() (int64, error) {
	if db == nil || !db.Migrator().HasTable(&model.MediaItem{}) {
		return 0, nil
	}
	type liteRow struct {
		ID          uint
		CreatedAt   time.Time
		ReleaseDate string
	}
	var rows []liteRow
	if err := db.Unscoped().Model(&model.MediaItem{}).
		Select("id", "created_at", "release_date").
		Where("release_date IS NOT NULL AND release_date != ''").
		Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}

	type pendingUpdate struct {
		id uint
		ts time.Time
	}
	pending := make([]pendingUpdate, 0, len(rows))
	for _, r := range rows {
		ts, ok := ParseMediaReleaseDate(r.ReleaseDate)
		if !ok {
			continue
		}
		if !r.CreatedAt.UTC().Equal(ts.UTC()) {
			pending = append(pending, pendingUpdate{id: r.ID, ts: ts})
		}
	}
	if len(pending) == 0 {
		return 0, nil
	}

	const batchSize = 200
	var updated int64
	for start := 0; start < len(pending); start += batchSize {
		end := start + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[start:end]
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, u := range chunk {
				if err := tx.Unscoped().Model(&model.MediaItem{}).
					Where("id = ?", u.id).
					UpdateColumn("created_at", u.ts).Error; err != nil {
					return err
				}
				updated++
			}
			return nil
		})
		if err != nil {
			return updated, err
		}
	}
	return updated, nil
}
