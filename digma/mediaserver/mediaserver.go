package mediaserver

import (
	"database/sql"
	"io"
	"log"
	"net/url"
	"sort"

	"github.com/valyala/fasthttp"

	"github.com/tomasen/fcgi_client"
)

// the Mediaserver does some nice conversion things for various media types
// initially uses some php stuff to do it...
type Mediaserver struct {
	db             *sql.DB
	fcgiProto      string
	fcgiAddr       string
	scriptFilename string
}

// Create a new Mediaserver
// db Database Handle
// fcgiProto Protocol for FCGI connection
// fcgiAddr Address for FCGI connection
func New(db *sql.DB, fcgiProto string, fcgiAddr string, scriptFilename string) *Mediaserver {
	mediaserver := &Mediaserver{
		db:             db,
		fcgiProto:      fcgiProto,
		fcgiAddr:       fcgiAddr,
		scriptFilename: scriptFilename}
	mediaserver.Init()
	return mediaserver
}

func (ms *Mediaserver) Init() (err error) {
	return
}

func (ms *Mediaserver) Handler(ctx *fasthttp.RequestCtx, collection string, signature string, function string, params []string) (err error) {
	sort.Strings(params)
	fcgi, err := fcgiclient.Dial(ms.fcgiProto, ms.fcgiAddr)
	if err != nil {
		log.Println(err)
		ctx.Error("Unable to connect to the backend", 502)
		return
	}
	parameters := url.Values{}
	parameters.Add("collection", collection)
	parameters.Add("signature", signature)
	parameters.Add("function", function)
	for _, value := range params {
		parameters.Add("params[]", value)
	}

	env := make(map[string]string)
	env["SCRIPT_FILENAME"] = ms.scriptFilename
	env["SERVER_SOFTWARE"] = "DIGMA Mediaserver"
	env["REMOTE_ADDR"] = "127.0.0.1"
	env["QUERY_STRING"] = parameters.Encode()
	resp, err := fcgi.Get(env)
	if err != nil {
		log.Println(err)
		ctx.Error("error querying backend", 500)
	}
	contentType := resp.Header.Get("Content-type")
	if contentType == "" {
		contentType = "text/html"
	}
	ctx.SetContentType(contentType)

	_, err = io.Copy(ctx, resp.Body)
	if err != nil {
		log.Println(err)
		ctx.Error("error getting content from backend", 500)
	}

	return
}
