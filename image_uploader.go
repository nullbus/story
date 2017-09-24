package story

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/russross/blackfriday"
)

// TistoryRenderer takes image link and upload image if possible.
// Overrides blackfriday.Renderer.Image()
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
