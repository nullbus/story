package story

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	try {
		var code = window.location.search.substring(1).split("&").filter(l => l.startsWith("code="));
		if (code.length === 1)
		{
			window.location.href = window.location.protocol + "//" + window.location.host + "/success?" + code[0].substring("code=".length);
		}
		else
		{
			throw new Error("failed to initialize!");
		}
	} catch (e) {
		document.getElementById("result").innerHTML = e.toString();
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
	confFile := filepath.Join(filepath.Dir(os.Args[0]), "story.conf")
	f, err := os.Open(confFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(c)
}

func (c *InitConfig) Save() error {
	confFile := filepath.Join(filepath.Dir(os.Args[0]), "story.conf")
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
	flag.StringVar(&c.ClientSecret, "secret", "", "tistory client secret")

	if err := flag.Parse(args); err != nil {
		return err
	}

	if c.ClientSecret == "" {
		return errors.New("no client secret specified")
	}

	return nil
}

func runOAuthServer(port int, path string, clientID string, clientSecret string, redirectURI string) (*http.Server, chan string) {
	chanResult := make(chan string)

	var server http.Server
	server.Addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	server.Handler = http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		log.Println("access", req.URL)
		if req.URL.Path[1:] == path {

			exchangeCode := req.URL.Query().Get("code")
			exchangeQuery := url.Values{}
			exchangeQuery.Add("client_id", clientID)
			exchangeQuery.Add("client_secret", clientSecret)
			exchangeQuery.Add("redirect_uri", redirectURI)
			exchangeQuery.Add("code", exchangeCode)
			exchangeQuery.Add("grant_type", "authorization_code")

			exchangeAddr := "https://www.tistory.com/oauth/access_token?" + exchangeQuery.Encode()
			log.Println(exchangeAddr)

			exchangeResp, err := http.Get(exchangeAddr)
			if err != nil {
				res.WriteHeader(http.StatusBadRequest)
				res.Write([]byte(fmt.Sprintf(`<script> alert('Authentication failed: %s'); setTimeout(window.close, 1); </script>`, err.Error())))
				return
			}

			// read body
			var buffer bytes.Buffer
			defer exchangeResp.Body.Close()

			if _, err := io.Copy(&buffer, exchangeResp.Body); err != nil {
				res.WriteHeader(http.StatusBadRequest)
				res.Write([]byte(fmt.Sprintf(`<script> alert('Authentication failed: %s'); setTimeout(window.close, 1); </script>`, err.Error())))
				return
			}

			if exchangeResp.StatusCode != http.StatusOK {
				res.WriteHeader(http.StatusBadRequest)
				res.Write([]byte(fmt.Sprintf(`<script> alert('Authentication failed: %s'); setTimeout(window.close, 1); </script>`, buffer.String())))
				return
			}

			res.WriteHeader(http.StatusOK)
			res.Write([]byte(`<script> alert('Authentication success'); setTimeout(window.close, 1); </script>`))

			chanResult <- strings.Split(buffer.String(), "=")[1]

		} else {
			http.NotFound(res, req)
		}
	})

	go server.ListenAndServe()

	return &server, chanResult
}

func (config *InitConfig) Authorize() error {
	// run server to accept redirect_uri
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/%s", config.RedirectPort, config.RedirectPath)
	server, chanString := runOAuthServer(config.RedirectPort, config.RedirectPath, config.ClientID, config.ClientSecret, redirectURI)
	defer server.Close()

	authQuery := url.Values{}
	authQuery.Add("response_type", "code")
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
