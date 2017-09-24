package story

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	return flag.Parse(args)
}

func runOAuthServer(port int, path string) (*http.Server, chan string) {
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

func (config *InitConfig) Authorize() error {
	// run server to accept redirect_uri
	server, chanString := runOAuthServer(config.RedirectPort, config.RedirectPath)
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
