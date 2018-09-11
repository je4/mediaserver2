package mediaserver

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"

	"github.com/tomasen/fcgi_client"
)

// the Mediaserver does some nice conversion things for various media types
// initially uses some php stuff to do it...
type Mediaserver struct {
	db             *sql.DB
	fcgiProto      string
	fcgiAddr       string
	scriptFilename string
	collections    *Collections
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

// constructor
func (ms *Mediaserver) Init() (err error) {
	ms.collections = NewCollections(ms.db)
	/*
	c, err := ms.collections.ById(10)
	if err != nil {
		log.Fatal(err)
	} 
	log.Println( c )
	*/
	return
}

// query handler
func (ms *Mediaserver) Handler(writer http.ResponseWriter, req *http.Request, collection string, signature string, action string, params []string) (err error) {
	sort.Strings(params)
	fcgi, err := fcgiclient.Dial(ms.fcgiProto, ms.fcgiAddr)
	if err != nil {
		log.Println(err)
		fmt.Fprintln(writer, "Unable to connect to the backend")
		writer.WriteHeader(502)
		//ctx.Error("Unable to connect to the backend", 502)
		return
	}
	parameters := url.Values{}
	parameters.Add("collection", collection)
	parameters.Add("signature", signature)
	parameters.Add("action", action)
	for _, value := range params {
		if value != "" {
			parameters.Add("params[]", value)
		}
	}

	env := map[string]string{
		"AUTH_TYPE":       "", // Not used
		"SCRIPT_FILENAME": ms.scriptFilename,
		"SERVER_SOFTWARE": "DIGMA Mediaserver/0.1",
		"REMOTE_ADDR":     req.RemoteAddr,
		"QUERY_STRING":    parameters.Encode(),
		"HOME":            "/",
		"HTTPS":           "on",
		"REQUEST_SCHEME":  "https",
		"SERVER_PROTOCOL": req.Proto,
		"REQUEST_METHOD":  req.Method,
		"FCGI_ROLE":       "RESPONDER",
		"REQUEST_URI":     req.RequestURI,
	}
	resp, err := fcgi.Get(env)
	if err != nil {
		log.Println(err)
		fmt.Fprintln(writer, "Unable to connect to the backend")
		writer.WriteHeader(500)
		return
	}
	contentType := resp.Header.Get("Content-type")
	if contentType == "" {
		contentType = "text/html"
	}
	writer.Header().Set("Content-type", contentType)

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		log.Println(err)
		fmt.Fprintln(writer, "error getting content from backend")
		writer.WriteHeader(500)
		return
	}

	return
}
