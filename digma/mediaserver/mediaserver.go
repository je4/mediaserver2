package mediaserver

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomasen/fcgi_client"
)

var (
	VERSION = "DIGMA Mediaserver 0.1"
)

// the Mediaserver does some nice conversion things for various media types
// initially uses some php stuff to do it...
type Mediaserver struct {
	db             *sql.DB
	fcgiProto      string
	fcgiAddr       string
	scriptFilename string
	collections    *Collections
	storages       *Storages
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
	ms.storages = NewStorages(ms.db)
	/*
		s, err := ms.storages.ById(9)
		if err != nil {
			log.Fatal(err)
		}
		log.Println( s )
	*/
	return
}

// query handler
func (ms *Mediaserver) Handler(writer http.ResponseWriter, req *http.Request, collection string, signature string, action string, params []string) (err error) {
	var (
		filebase    string
		path        string
		mimetype    string
		jwtkey      sql.NullString
		paramstring string
	)
	sort.Strings(params)

	coll, err := ms.collections.ByName(collection)
	paramstring = strings.Trim(strings.Join(params, "/"), "/")
	rows, err := ms.db.Query("select filebase, path, mimetype, jwtkey from fullcache WHERE collection_id=? AND signature=? and action=? AND param=?", coll.id, signature, action, paramstring)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&filebase, &path, &mimetype, &jwtkey)
		if err != nil {
			log.Fatal(err)
			break
		}
		uri := strings.TrimRight(filebase, "/") + "/" + strings.TrimLeft(path, "/")
		url, err := url.Parse(uri)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := os.Stat(url.Path); err == nil {
			filePath := url.Path
			_, fileName := filepath.Split(filePath)

			fileStat, err := os.Stat(filePath)
			if err != nil {
				fmt.Println(err)
				return err
			}

			if fileStat.IsDir() {
				writer.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(writer, "<html><body style='font-size:100px'>Zugriff auf Verzeichnis %s verweigert</body></html>", fileName)
				return err
			}

			file, err := os.Open(filePath)
			if err != nil {
				fmt.Printf("%s not found\n", filePath)
				writer.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(writer, "<html><body style='font-size:100px'>Die Kollektion enthält keine Datei %s</body></html>", fileName)
				return err
			}
			defer file.Close()

			t := fileStat.ModTime()
			writer.Header().Set("Server", VERSION)
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			http.ServeContent(writer, req, fileName, t, file)

			return nil
		}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}

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
