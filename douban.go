package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/resty.v1"
)

const (
	desktopUA   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
	mobileUA    = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	maxMetaRune = 34
)

var yearRegex = regexp.MustCompile(`^\d{4}(-\d{1,2})?(-\d{1,2})?$`)

type UrlItem struct {
	DesktopURL string
	MobileURL  string
	Category   string
}

type SearchResultItem struct {
	Title         string
	OriginScore   string
	MetaInfo      string
	Summary       string
	Url           string
	FullStarCount int
	HalfStarCount int
	ErrMsg        string
}

type AlfredText struct {
	Copy      string `json:"copy,omitempty"`
	LargeType string `json:"largetype,omitempty"`
}

type AlfredItem struct {
	Type         string      `json:"type,omitempty"`
	Title        string      `json:"title"`
	Subtitle     string      `json:"subtitle"`
	Arg          string      `json:"arg"`
	QuickLookURL string      `json:"quicklookurl,omitempty"`
	Text         *AlfredText `json:"text,omitempty"`
	Icon         struct {
		Path string `json:"path"`
	} `json:"icon"`
}

var urlMapping = map[string]UrlItem{
	"book": {
		DesktopURL: "https://www.douban.com/search?cat=%s&q=%s",
		MobileURL:  "https://m.douban.com/search/?type=%s&query=%s",
		Category:   "1001",
	},
	"movie": {
		DesktopURL: "https://www.douban.com/search?cat=%s&q=%s",
		MobileURL:  "https://m.douban.com/search/?type=%s&query=%s",
		Category:   "1002",
	},
	"music": {
		DesktopURL: "https://www.douban.com/search?cat=%s&q=%s",
		MobileURL:  "https://m.douban.com/search/?type=%s&query=%s",
		Category:   "1003",
	},
}

func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

func formatSubjectCast(rawCast string, searchType string) string {
	sep := " / "
	if !strings.Contains(rawCast, sep) {
		sep = "/"
	}
	rawParts := strings.Split(rawCast, sep)
	parts := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		cleaned := strings.TrimSpace(p)
		if cleaned == "" {
			continue
		}
		if searchType == "movie" && (strings.HasPrefix(cleaned, "原名:") || strings.HasPrefix(cleaned, "原名：")) {
			continue
		}
		parts = append(parts, cleaned)
	}

	if len(parts) == 0 {
		return ""
	}

	switch searchType {
	case "book":
		// 图书常见格式: [作者, (译者), 出版社, 出版年, (定价)]
		// 优先保留：作者 / 出版社 / 年份（控制在 3 项以内，避免过长）
		if len(parts) >= 4 {
			lastIdx := len(parts) - 1
			// 如果最后一项是价格（非年份且包含数字/元），往前找年份和出版社
			if !yearRegex.MatchString(parts[lastIdx]) && yearRegex.MatchString(parts[lastIdx-1]) {
				lastIdx--
			}
			if lastIdx >= 3 && yearRegex.MatchString(parts[lastIdx]) {
				parts = []string{parts[0], parts[lastIdx-1], parts[lastIdx]}
			} else {
				parts = parts[:3]
			}
		}
	case "movie":
		// 影视常见格式: [导演, 主演..., 年份]
		// 若导演与第一位主演同名（如自导自演），去重避免显示 "姜文 / 姜文 / 2010"
		dedup := make([]string, 0, len(parts))
		for _, p := range parts {
			if len(dedup) > 0 && dedup[len(dedup)-1] == p {
				continue
			}
			dedup = append(dedup, p)
		}
		parts = dedup
		if len(parts) > 3 {
			last := parts[len(parts)-1]
			if yearRegex.MatchString(last) {
				parts = []string{parts[0], parts[1], last}
			} else {
				parts = parts[:3]
			}
		}
	case "music":
		// 音乐常见格式: [表演者, 流派, 年份]
		if len(parts) > 3 {
			parts = parts[:3]
		}
	}

	return truncateRunes(strings.Join(parts, " / "), maxMetaRune)
}

func extractDirectURL(href string) string {
	u, err := url.Parse(href)
	if err == nil {
		if target := u.Query().Get("url"); target != "" {
			return target
		}
	}
	return href
}

func parseAllStarClass(sel *goquery.Selection) (int, int) {
	starSpan := sel.Find("span[class^='allstar']").First()
	cls, exists := starSpan.Attr("class")
	if !exists {
		return 0, 0
	}
	for _, token := range strings.Fields(cls) {
		if strings.HasPrefix(token, "allstar") {
			numStr := strings.TrimPrefix(token, "allstar")
			val, err := strconv.Atoi(numStr)
			if err == nil && val > 0 {
				full := val / 10
				half := (val % 10) / 5
				return full, half
			}
		}
	}
	return 0, 0
}

func getDesktopItems(v UrlItem, searchType string, searchString string) ([]SearchResultItem, error) {
	targetURL := fmt.Sprintf(v.DesktopURL, v.Category, url.QueryEscape(searchString))
	client := resty.New().
		SetTimeout(8 * time.Second).
		SetHeader("User-Agent", desktopUA).
		SetHeader("Referer", "https://www.douban.com/").
		SetHeader("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.R().Get(targetURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode())
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
	if err != nil {
		return nil, err
	}

	results := make([]SearchResultItem, 0)
	doc.Find("div.result").Each(func(_ int, sel *goquery.Selection) {
		aNode := sel.Find("div.content div.title h3 a").First()
		title := strings.TrimSpace(aNode.Text())
		href, exists := aNode.Attr("href")
		if !exists || href == "" || title == "" {
			return
		}

		directURL := extractDirectURL(href)
		ratingInfo := sel.Find("div.rating-info")
		score := strings.TrimSpace(ratingInfo.Find("span.rating_nums").Text())
		if score == "" {
			// 例如 "(暂无评分)" 或 "(评价人数不足)"
			firstSpan := strings.TrimSpace(ratingInfo.Find("span").Not("[class]").First().Text())
			firstSpan = strings.Trim(firstSpan, "()")
			if firstSpan != "" && !strings.HasSuffix(firstSpan, "人评价") {
				score = firstSpan
			} else {
				score = "暂无评分"
			}
		}

		fullStar, halfStar := parseAllStarClass(ratingInfo)
		rawCast := strings.TrimSpace(ratingInfo.Find("span.subject-cast").Text())
		metaInfo := formatSubjectCast(rawCast, searchType)
		summary := strings.TrimSpace(sel.Find("div.content > p").Text())

		results = append(results, SearchResultItem{
			Title:         title,
			OriginScore:   score,
			MetaInfo:      metaInfo,
			Summary:       summary,
			Url:           directURL,
			FullStarCount: fullStar,
			HalfStarCount: halfStar,
		})
	})

	return results, nil
}

func getMobileItems(v UrlItem, searchType string, searchString string) ([]SearchResultItem, error) {
	targetURL := fmt.Sprintf(v.MobileURL, v.Category, url.QueryEscape(searchString))
	client := resty.New().
		SetTimeout(8 * time.Second).
		SetHeader("User-Agent", mobileUA).
		SetHeader("Referer", "https://m.douban.com/").
		SetHeader("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.R().Get(targetURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode())
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
	if err != nil {
		return nil, err
	}

	r := make([]SearchResultItem, 0)
	doc.Find("ul.search_results_subjects > li").Each(func(_ int, sel *goquery.Selection) {
		aNode := sel.Find("a").First()
		href, exists := aNode.Attr("href")
		if !exists || href == "" {
			return
		}
		href = strings.TrimPrefix(href, "/"+searchType)

		title := strings.TrimSpace(sel.Find(".subject-title").Text())
		if title == "" {
			title = strings.TrimSpace(sel.Find("a > div > span").Text())
		}
		if title == "" {
			return
		}

		originScore := strings.TrimSpace(sel.Find("a > div > p > span").Last().Text())
		fullStar := len(sel.Find(".rating-star-small-full").Nodes)
		halfStar := len(sel.Find(".rating-star-small-half").Nodes)

		r = append(r, SearchResultItem{
			Title:         title,
			OriginScore:   originScore,
			Url:           href,
			FullStarCount: fullStar,
			HalfStarCount: halfStar,
		})
	})

	return r, nil
}

func getItems(searchType string, searchString string) *[]SearchResultItem {
	v, ok := urlMapping[searchType]
	if !ok {
		return &[]SearchResultItem{{Title: "不支持的搜索类型", ErrMsg: searchType}}
	}

	// 优先使用豆瓣桌面端搜索（包含作者/出版社、导演/演员、表演者/流派/年份等丰富信息）
	items, err := getDesktopItems(v, searchType, searchString)
	if err != nil || len(items) == 0 {
		// 若桌面端接口异常或无结果，回退尝试移动端接口
		mobileItems, mobileErr := getMobileItems(v, searchType, searchString)
		if mobileErr != nil && err != nil {
			return &[]SearchResultItem{{
				Title:  "网络或豆瓣接口异常",
				ErrMsg: fmt.Sprintf("%v，回车在浏览器中打开豆瓣搜索", err),
				Url:    fmt.Sprintf("https://www.douban.com/search?q=%s", url.QueryEscape(searchString)),
			}}
		}
		if len(mobileItems) > 0 {
			items = mobileItems
		}
	}

	if len(items) == 0 {
		return &[]SearchResultItem{{
			Title:  fmt.Sprintf("未找到“%s”的相关结果", searchString),
			ErrMsg: "回车在浏览器中打开豆瓣网页搜索",
			Url:    fmt.Sprintf("https://www.douban.com/search?q=%s", url.QueryEscape(searchString)),
		}}
	}

	return &items
}

func generateResponse(items *[]SearchResultItem, searchType string) {
	baseUrl := fmt.Sprintf("https://%s.douban.com", searchType)
	r := make([]AlfredItem, 0)
	if items != nil {
		for _, i := range *items {
			var subTitle string
			if i.ErrMsg != "" {
				subTitle = i.ErrMsg
			} else {
				stars := strings.Repeat("⭐", i.FullStarCount) + strings.Repeat("⚡", i.HalfStarCount)
				ratingPart := strings.TrimSpace(stars + " " + i.OriginScore)
				if i.MetaInfo != "" {
					subTitle = ratingPart + " · " + i.MetaInfo
				} else {
					subTitle = ratingPart
				}
			}

			arg := i.Url
			if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") && arg != "" {
				if !strings.HasPrefix(arg, "/") {
					arg = "/" + arg
				}
				arg = fmt.Sprintf("%s%s", baseUrl, arg)
			}

			var textObj *AlfredText
			if i.ErrMsg == "" {
				largeLines := []string{i.Title, subTitle}
				if i.Summary != "" {
					largeLines = append(largeLines, "", i.Summary)
				}
				textObj = &AlfredText{
					Copy:      arg,
					LargeType: strings.Join(largeLines, "\n"),
				}
			}

			r = append(r, AlfredItem{
				Title:        i.Title,
				Subtitle:     subTitle,
				Arg:          arg,
				QuickLookURL: arg,
				Text:         textObj,
				Icon: struct {
					Path string `json:"path"`
				}{
					Path: fmt.Sprintf("imgs/%s.png", searchType),
				},
			})
		}
	}
	finalRes, _ := json.Marshal(struct {
		Items []AlfredItem `json:"items"`
	}{
		Items: r,
	})
	fmt.Println(string(finalRes))
}

func main() {
	if len(os.Args) < 2 {
		generateResponse(&[]SearchResultItem{{Title: "参数缺失", ErrMsg: "请指定搜索类型 (book/movie/music)"}}, "book")
		return
	}
	searchType := os.Args[1]
	query := strings.TrimSpace(strings.Join(os.Args[2:], " "))
	if query == "" {
		generateResponse(&[]SearchResultItem{}, searchType)
		return
	}
	generateResponse(getItems(searchType, query), searchType)
}
