package story

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// Info gets information about blog.
func Info(accessToken string) (string, error) {
	query := url.Values{}
	query.Add("access_token", accessToken)
	query.Add("output", "json")

	resp, err := http.Get("https://www.tistory.com/apis/blog/info?" + query.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(parseError(resp.Body))
	}

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, resp.Body)
	return buffer.String(), err
}
