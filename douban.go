package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"gopkg.in/resty.v1"
)

const userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

type UrlItem struct {
	URL      string
	Category string
}

type SearchResultItem struct {
	Title         string
	OriginScore   string
	Url           string
	FullStarCount int
	HalfStarCount int
	ErrMsg        string
}

type AlfredItem struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Arg      string `json:"arg"`
	Icon     struct {
		Path string `json:"path"`
	} `json:"icon"`
}

var urlMapping = map[string]UrlItem{
	"book": {
		URL:      "https://m.douban.com/search/?type=%s&query=%s",
		Category: "1001",
	},
	"movie": {
		URL:      "https://m.douban.com/search/?type=%s&query=%s",
		Category: "1002",
	},
	"music": {
		URL:      "https://m.douban.com/search/?type=%s&query=%s",
		Category: "1003",
	},
}

func getItems(searchType string, searchString string) *[]SearchResultItem {
	v, ok := urlMapping[searchType]
	if !ok {
		return &[]SearchResultItem{{Title: "不支持的搜索类型", ErrMsg: searchType}}
	}

	targetURL := fmt.Sprintf(v.URL, v.Category, url.QueryEscape(searchString))
	client := resty.New().
		SetTimeout(10 * time.Second).
		SetHeader("User-Agent", userAgent).
		SetHeader("Referer", "https://m.douban.com/").
		SetHeader("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.R().Get(targetURL)
	if err != nil {
		return &[]SearchResultItem{{Title: "网络错误", ErrMsg: err.Error()}}
	}
	if resp.StatusCode() != http.StatusOK {
		return &[]SearchResultItem{{
			Title:  "豆瓣请求异常",
			ErrMsg: fmt.Sprintf("HTTP %d，点击在浏览器中打开豆瓣搜索", resp.StatusCode()),
			Url:    fmt.Sprintf("https://www.douban.com/search?q=%s", url.QueryEscape(searchString)),
		}}
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body()))
	if err != nil {
		return &[]SearchResultItem{{Title: "解析结果失败", ErrMsg: err.Error()}}
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

	if len(r) == 0 {
		return &[]SearchResultItem{{
			Title:  fmt.Sprintf("未找到“%s”的相关结果", searchString),
			ErrMsg: "回车在浏览器中打开豆瓣网页搜索",
			Url:    fmt.Sprintf("https://www.douban.com/search?q=%s", url.QueryEscape(searchString)),
		}}
	}

	return &r
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
				subTitle = strings.TrimSpace(strings.Repeat("⭐", i.FullStarCount) + strings.Repeat("⚡", i.HalfStarCount) + " " + i.OriginScore)
			}

			arg := i.Url
			if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") && arg != "" {
				if !strings.HasPrefix(arg, "/") {
					arg = "/" + arg
				}
				arg = fmt.Sprintf("%s%s", baseUrl, arg)
			}

			r = append(r, AlfredItem{
				Title:    i.Title,
				Subtitle: subTitle,
				Arg:      arg,
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
