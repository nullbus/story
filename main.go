package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/russross/blackfriday"
)

const AUTH_REDIRECT_CONTENT = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta http-equiv="X-UA-Compatible" content="ie=edge">
    <title>Authentication Result</title>
</head>
<body>
    <div id="result">
    </div>
    <script type="text/javascript">
    if (window.location.hash == "")
    {
        document.getElementById("result").innerHTML = "failed to initialize!"
    }
    else
    {
        window.location.href = window.location.protocol + "//" + window.location.host + "/success?" + window.location.hash.substr(1);
    }
    </script>
</body>
</html>
`

type InitConfig struct {
	RedirectPort int
	RedirectPath string
	ClientID     string
	ClientSecret string
	AccessToken  string
}

func (c *InitConfig) Load() error {
	confFile := filepath.Join(filepath.Dir(os.Args[0]), "conf.yml")
	f, err := os.Open(confFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(c)
}

func (c *InitConfig) Save() error {
	confFile := filepath.Join(filepath.Dir(os.Args[0]), "conf.yml")
	f, err := os.Create(confFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(c)
}

func (c *InitConfig) Parse(args []string) error {
	flag := flag.NewFlagSet("story init", flag.ExitOnError)
	flag.IntVar(&c.RedirectPort, "rdport", 18769, "redirection uri port")
	flag.StringVar(&c.RedirectPath, "rdpath", "oauth_result", "path of redirection uri")
	return flag.Parse(args)
}

type ShowConfig struct {
	BlogName string
	PostID   string
}

func (c *ShowConfig) Parse(args []string) error {
	flag := flag.NewFlagSet("story show", flag.ExitOnError)
	flag.StringVar(&c.BlogName, "blog", "", "tistory blog name, ex> {blog}.tistory.com")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "story show -blog=[blog id] [options] postID")
		flag.PrintDefaults()
	}

	if err := flag.Parse(args); err != nil {
		return err
	}

	if flag.NArg() == 0 {
		return errors.New("too few arguments")
	}

	c.PostID = flag.Arg(0)

	if c.BlogName == "" {
		return errors.New("missing blog name")
	}

	return nil
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

type TistoryRenderer struct {
	blackfriday.Renderer
	BlogName    string
	AccessToken string
	WorkingDir  string
}

func (t *TistoryRenderer) Image(out *bytes.Buffer, link []byte, title []byte, alt []byte) {
	// check if file is not local file
	if _, err := url.Parse(string(link)); err != nil {
		t.Renderer.Image(out, link, title, alt)
		return
	}

	uploadFailed := func(err error) {
		log.Println("uploading image file error:", err.Error())
		log.Println("skip uploading file", string(link))
		t.Renderer.Image(out, link, title, alt)
	}

	f, err := os.Open(path.Join(t.WorkingDir, string(link)))
	if err != nil {
		uploadFailed(err)
		return
	}
	defer f.Close()

	// upload image file
	var payloadForm bytes.Buffer
	mpWriter := multipart.NewWriter(&payloadForm)
	if err := mpWriter.WriteField("access_token", t.AccessToken); err != nil {
		uploadFailed(err)
		return
	}

	if err := mpWriter.WriteField("blogName", t.BlogName); err != nil {
		uploadFailed(err)
		return
	}

	if err := mpWriter.WriteField("output", "json"); err != nil {
		uploadFailed(err)
		return
	}

	fileWriter, err := mpWriter.CreateFormFile("uploadedfile", path.Base(string(link)))
	if err != nil {
		uploadFailed(err)
		return
	}

	if _, err := io.Copy(fileWriter, f); err != nil {
		uploadFailed(err)
		return
	}

	// flush body content
	mpWriter.Close()

	// do request
	resp, err := http.Post("https://www.tistory.com/apis/post/attach", mpWriter.FormDataContentType(), &payloadForm)
	if err != nil {
		uploadFailed(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		uploadFailed(errors.New(parseError(resp.Body)))
		return
	}

	var responseBody struct {
		Tistory struct {
			URL      string `json:"url"`
			Replacer string `json:"replacer"`
		} `json:"tistory"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		uploadFailed(err)
		return
	}

	log.Println("image url is", responseBody.Tistory.URL)
	out.WriteString(responseBody.Tistory.Replacer)
}

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

func RunOAuthServer(port int, path string) (*http.Server, chan string) {
	chanResult := make(chan string)

	var server http.Server
	server.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	server.Handler = http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		log.Println("access", req.URL)
		if req.URL.Path == "/success" {
			res.WriteHeader(http.StatusOK)
			res.Write([]byte(`<script> alert('Authentication success'); setTimeout(window.close, 1); </script>`))
			chanResult <- req.URL.Query().Get("access_token")

		} else if req.URL.Path[1:] == path {
			res.Header().Set("Content-Type", "text/html")
			res.WriteHeader(http.StatusOK)
			res.Write([]byte(AUTH_REDIRECT_CONTENT))

		} else {
			http.NotFound(res, req)
		}
	})

	go server.ListenAndServe()

	return &server, chanResult
}

func Authorize(config *InitConfig) error {
	// run server to accept redirect_uri
	server, chanString := RunOAuthServer(config.RedirectPort, config.RedirectPath)
	defer server.Close()

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/%s", config.RedirectPort, config.RedirectPath)
	authQuery := url.Values{}
	authQuery.Add("response_type", "token")
	authQuery.Add("client_id", config.ClientID)
	authQuery.Add("redirect_uri", redirectURI)

	authAddr := "https://www.tistory.com/oauth/authorize?" + authQuery.Encode()
	log.Println(authAddr)

	// open browser to authorize
	exec.Command("rundll32", "url.dll,FileProtocolHandler", authAddr).Start()

	// wait for access token
	timer := time.NewTimer(10 * time.Second)
	select {
	case config.AccessToken = <-chanString:
		log.Println("authorization code is", config.AccessToken)
	case <-timer.C:
		return errors.New("OAuth timeout")
	}

	return config.Save()
}

func Info(accessToken string) error {
	query := url.Values{}
	query.Add("access_token", accessToken)
	query.Add("output", "json")

	resp, err := http.Get("https://www.tistory.com/apis/blog/info?" + query.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(parseError(resp.Body))
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	return err
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

func Edit(config *EditConfig, accessToken string) error {
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

func Upload(config *PostConfig, accessToken string) error {
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

func usageAndExit() {
	write := func(args ...interface{}) { fmt.Fprintln(os.Stderr, args...) }
	write("Usage:")
	write("  story init")
	write("  story show")
	write("  story post")
	write("  story edit")

	os.Exit(1)
}

func main() {
	if len(os.Args) == 1 {
		usageAndExit()
	}

	switch os.Args[1] {
	case "init":
		var config InitConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
			return
		}

		for {
			fmt.Print("Client ID: ")
			fmt.Scanf("%s\n", &config.ClientID)

			if config.ClientID != "" {
				break
			}
		}

		for {
			fmt.Print("Client Secret: ")
			fmt.Scanf("%s\n", &config.ClientSecret)

			if config.ClientSecret != "" {
				break
			}
		}

		if err := Authorize(&config); err != nil {
			log.Fatalln(err)
			return
		}

	case "auth":
		var config InitConfig
		if err := config.Load(); err != nil {
			log.Fatalln("failed to load config file, try `story init` first")
		}

		if err := Authorize(&config); err != nil {
			log.Fatalln(err)
		}

	case "info":
		var config InitConfig
		if err := config.Load(); err != nil {
			log.Fatalln("failed to load config file, try `story init` first")
		}

		if err := Info(config.AccessToken); err != nil {
			log.Fatalln(err)
		}

	case "show":
		var baseConfig InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Fatalln(err)
		}

		var config ShowConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
		}

		post, err := FindPost(baseConfig.AccessToken, config.BlogName, config.PostID)
		if err != nil {
			log.Fatalln(err)
		}

		fmt.Printf("%+v\n", post)

	case "edit":
		var baseConfig InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Println("failed to load config file, try `story init` first")
			os.Exit(1)
			return
		}

		var config EditConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
		}

		if err := Edit(&config, baseConfig.AccessToken); err != nil {
			log.Fatalln(err)
		}

	case "post":
		var baseConfig InitConfig
		if err := baseConfig.Load(); err != nil {
			log.Println("failed to load config file, try `story init` first")
			os.Exit(1)
			return
		}

		var config PostConfig
		if err := config.Parse(os.Args[2:]); err != nil {
			log.Fatalln(err)
			return
		}

		if err := Upload(&config, baseConfig.AccessToken); err != nil {
			log.Fatalln(err)
		}
	default:
		usageAndExit()
	}

}
