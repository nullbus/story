package story

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/russross/blackfriday"
)

const (
	// copied from github.com/russross/blackfriday/markdown.go
	commonHtmlFlags = blackfriday.HTML_USE_XHTML |
		blackfriday.HTML_USE_SMARTYPANTS |
		blackfriday.HTML_SMARTYPANTS_FRACTIONS |
		blackfriday.HTML_SMARTYPANTS_DASHES |
		blackfriday.HTML_SMARTYPANTS_LATEX_DASHES

	commonExtensions = 0 |
		blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
		blackfriday.EXTENSION_TABLES |
		blackfriday.EXTENSION_FENCED_CODE |
		blackfriday.EXTENSION_AUTOLINK |
		blackfriday.EXTENSION_STRIKETHROUGH |
		blackfriday.EXTENSION_SPACE_HEADERS |
		blackfriday.EXTENSION_HEADER_IDS |
		blackfriday.EXTENSION_BACKSLASH_LINE_BREAK |
		blackfriday.EXTENSION_DEFINITION_LISTS
)

// "item":{
// 	"id":"1",
// 	"title":"티스토리 OAuth2.0 API 오픈!",
// 	"content":"안녕하세요 Tistory API 입니다.<br><br>이번에 Third-party Developer 용 <b>Tistory OAuth 2.0 API</b> 가 오픈됩니다.<br>Tistory 회원이라면, 여러분의 모든 app에 자유롭게 활용하실 수 있습니다.<br><br><div class="\"imageblock" center\"="" style="\"text-align:" center;="" clear:="" both;\"=""><img src="\"http://cfile10.uf.tistory.com/image/156987414DAF9799227B34\""></div><br><p></p>많은 기대와 사랑 부탁드립니다. <br> ",
// 	"categoryId":"0",
// 	"postUrl":"http://oauth.tistory.com/1",
// 	"visibility":"0",
// 	"acceptComment":"1",
// 	"acceptTrackback":"1",
// 	"tags : {
// 		tag : ["open", "api"]
// 	},
// 	"comments":"0",
// 	"trackbacks":"0",
// 	"date":"1303352668"
// }
type TistoryPost struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Content         string `json:"content"`
	CategoryID      int    `json:"categoryId,string"`
	PostURL         string `json:"postUrl"`
	Visibility      int    `json:"visibility,string"`
	AcceptComment   int    `json:"acceptCommnet,string"`
	AcceptTrackback int    `json:"acceptTrackback,string"`
	Comments        int    `json:"comments,string"`
	Trackbacks      int    `json:"trackbacks,string"`
	Date            string `json:"date"`
	// Tags            struct {
	// Tag []string `json:"tag"`
	// } `json:"tags"`
}

func parseError(r io.Reader) string {
	var responseBody struct {
		Tistory struct {
			Status       string `json:"status"`
			ErrorMessage string `json:"error_message"`
		}
	}

	if err := json.NewDecoder(r).Decode(&responseBody); err != nil {
		return err.Error()
	}

	return fmt.Sprintf("code %s: %s", responseBody.Tistory.Status, responseBody.Tistory.ErrorMessage)
}

func FindPost(accessToken string, blogName, postID string) (*TistoryPost, error) {
	query := url.Values{}
	query.Add("access_token", accessToken)
	query.Add("blogName", blogName)
	query.Add("postId", postID)
	query.Add("output", "json")

	resp, err := http.Get("https://www.tistory.com/apis/post/read?" + query.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(parseError(resp.Body))
	}

	var post struct {
		Tistory struct {
			Item TistoryPost `json:"item"`
		} `json:"tistory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&post); err != nil {
		return nil, err
	}

	return &post.Tistory.Item, nil
}

type PostConfig struct {
	BlogName string
	Title    string
	File     string
	DryRun   bool
}

func (c *PostConfig) Parse(args []string) error {
	flag := flag.NewFlagSet("story post", flag.ExitOnError)
	flag.StringVar(&c.BlogName, "blog", "", "tistory blog name, ex> {blog}.tistory.com")
	flag.BoolVar(&c.DryRun, "n", false, "actually do nothing")
	flag.Usage = func() {
		fmt.Println("story post -blog=[blog id] [title] [markdown file or directory]")
		flag.PrintDefaults()
	}

	if err := flag.Parse(args); err != nil {
		return err
	}

	if c.BlogName == "" {
		return errors.New("missing blog name")
	}

	c.File = filepath.ToSlash(flag.Arg(1))
	if _, err := os.Stat(c.File); err != nil {
		return err
	}

	c.Title = flag.Arg(0)
	if c.Title == "" {
		return errors.New("missing title")
	}

	return nil
}

func (config *PostConfig) Do(accessToken string) error {
	query := url.Values{}
	query.Add("access_token", accessToken)
	query.Add("blogName", config.BlogName)
	query.Add("title", config.Title)
	query.Add("output", "json")

	appendFile := func(content io.Writer, filename string) error {
		filename = filepath.ToSlash(filename)
		fileContent, err := ioutil.ReadFile(filename)
		if err != nil {
			return err
		}

		renderer := TistoryRenderer{
			Renderer:    blackfriday.HtmlRenderer(commonHtmlFlags, "", ""),
			AccessToken: accessToken,
			BlogName:    config.BlogName,
			WorkingDir:  path.Dir(filename),
		}

		if _, err = content.Write([]byte(`<div class="markdown">`)); err != nil {
			return err
		}
		if _, err = content.Write(blackfriday.Markdown(fileContent, &renderer, commonExtensions)); err != nil {
			return err
		}
		if _, err = content.Write([]byte(`</div>`)); err != nil {
			return err
		}

		return err
	}

	if stat, _ := os.Stat(config.File); stat.IsDir() {
		files, err := filepath.Glob(path.Join(config.File, "*.md"))
		if err != nil {
			return err
		} else if len(files) == 0 {
			return errors.New("no .md files found")
		}

		var buffer bytes.Buffer
		for i := range files {
			log.Println("reading", files[i])
			if err := appendFile(&buffer, files[i]); err != nil {
				return err
			}
		}

		query.Add("content", buffer.String())
	} else {
		var buffer bytes.Buffer
		log.Println("reading", config.File)
		if err := appendFile(&buffer, config.File); err != nil {
			return err
		}

		query.Add("content", buffer.String())
	}

	if !config.DryRun {
		resp, err := http.Post("https://www.tistory.com/apis/post/write", "application/x-www-form-urlencoded", bytes.NewBufferString(query.Encode()))
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errors.New(parseError(resp.Body))
		}

		var respBody struct {
			Tistory struct {
				URL string `json:"url"`
			} `json:"tistory"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
			return err
		}

		log.Println("post url:", respBody.Tistory.URL)
	}

	return nil
}

type EditConfig struct {
	BlogName string
	Title    string
	File     string
	PostID   string
	DryRun   bool
}

func (c *EditConfig) Parse(args []string) error {
	flag := flag.NewFlagSet("story edit", flag.ExitOnError)
	flag.StringVar(&c.BlogName, "blog", "", "tistory blog name, ex> {blog}.tistory.com")
	flag.StringVar(&c.Title, "title", "", "if specified, also change the title")
	flag.StringVar(&c.File, "content", "", "if specified, update the content")
	flag.BoolVar(&c.DryRun, "n", false, "actually do nothing")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: story edit [options] postID")
		flag.PrintDefaults()
	}

	if err := flag.Parse(args); err != nil {
		return err
	}

	if flag.NArg() == 0 {
		flag.Usage()
		return errors.New("missing post id")
	}

	if c.BlogName == "" {
		return errors.New("missing blog name")
	}

	if c.File == "" && c.Title == "" {
		return errors.New("nothing to do")
	}

	c.File = filepath.ToSlash(c.File)
	if _, err := os.Stat(c.File); err != nil {
		return err
	}

	c.PostID = flag.Arg(0)
	return nil
}

func (config *EditConfig) Do(accessToken string) error {
	post, err := FindPost(accessToken, config.BlogName, config.PostID)
	if err != nil {
		return err
	}

	title := post.Title
	if config.Title != "" {
		title = config.Title
	}

	content := post.Content

	if config.File != "" {
		appendFile := func(content io.Writer, filename string) error {
			filename = filepath.ToSlash(filename)
			fileContent, err := ioutil.ReadFile(filename)
			if err != nil {
				return err
			}

			renderer := TistoryRenderer{
				Renderer:    blackfriday.HtmlRenderer(commonHtmlFlags, "", ""),
				AccessToken: accessToken,
				BlogName:    config.BlogName,
				WorkingDir:  path.Dir(filename),
			}

			if _, err = content.Write([]byte(`<div class="markdown">`)); err != nil {
				return err
			}
			if _, err = content.Write(blackfriday.Markdown(fileContent, &renderer, commonExtensions)); err != nil {
				return err
			}
			if _, err = content.Write([]byte(`</div>`)); err != nil {
				return err
			}

			return nil
		}

		if stat, _ := os.Stat(config.File); stat.IsDir() {
			files, err := filepath.Glob(path.Join(config.File, "*.md"))
			if err != nil {
				return err
			} else if len(files) == 0 {
				return errors.New("no .md files found")
			}

			var buffer bytes.Buffer
			for i := range files {
				log.Println("reading", files[i])
				if err := appendFile(&buffer, files[i]); err != nil {
					return err
				}
			}

			content = buffer.String()
		} else {
			var buffer bytes.Buffer
			log.Println("reading", config.File)
			if err := appendFile(&buffer, config.File); err != nil {
				return err
			}

			content = buffer.String()
		}
	}

	if !config.DryRun {
		query := url.Values{}
		query.Add("access_token", accessToken)
		query.Add("blogName", config.BlogName)
		query.Add("title", title)
		query.Add("content", content)
		query.Add("postId", config.PostID)
		query.Add("output", "json")

		resp, err := http.Post("https://www.tistory.com/apis/post/modify", "application/x-www-form-urlencoded", bytes.NewBufferString(query.Encode()))
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return errors.New(parseError(resp.Body))
		}

		var respBody struct {
			Tistory struct {
				URL string `json:"url"`
			} `json:"tistory"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
			return err
		}

		log.Println("post url:", respBody.Tistory.URL)
	}

	return nil
}
