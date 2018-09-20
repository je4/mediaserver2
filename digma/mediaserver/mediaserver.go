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
	"strconv"
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
	alias          string
	subprefix      string
	storages       *Storages
	proxyURL       string
	iiifBase       string
	iiifAlias      string
}

// Create a new Mediaserver
// db Database Handle
// fcgiProto Protocol for FCGI connection
// fcgiAddr Address for FCGI connection
func New(db *sql.DB, fcgiProto string, fcgiAddr string, scriptFilename string, alias string, subprefix string, proxyURL string, iiifBase string, iiifAlias string) *Mediaserver {
	mediaserver := &Mediaserver{
		db:             db,
		fcgiProto:      fcgiProto,
		fcgiAddr:       fcgiAddr,
		alias:          alias,
		subprefix:      subprefix,
		scriptFilename: scriptFilename,
		proxyURL:       proxyURL,
		iiifBase:       iiifBase,
		iiifAlias:      iiifAlias}
	mediaserver.Init()
	return mediaserver
}

// constructor
func (ms *Mediaserver) Init() (err error) {
	ms.collections = NewCollections(ms.db)
	ms.storages = NewStorages(ms.db)
	return
}

// IIIF handler
func (ms *Mediaserver) HandlerIIIF(writer http.ResponseWriter, req *http.Request, file string, params string, token string) (err error) {
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	// token format: <storageid>_<token>
	tokenParts := strings.SplitN(token, "_", 2)
	storageid, err := strconv.Atoi(tokenParts[0])
	if err != nil {
		writer.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(writer, "<html><body><h3>Invalid token - no storageid: %s</h3></body></html>", token)
		return err
	}
	storage, err := ms.storages.ById(storageid)
	if err != nil {
		writer.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(writer, "<html><body><h3>Invalid token - storage #%u not found: %s</h3></body></html>", storageid, token)
		return err
	}
	if storage.secret.Valid {
		token := tokenParts[1]
		sub := strings.ToLower(strings.TrimRight(ms.subprefix+file, "/"))
		ok, err := CheckJWT(token, storage.secret.String, sub)
		if !ok {
			writer.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(writer, "<html><body><h3>invalid access token: %s</h3></body></html>", err)
			return err
		}
	}
	iiifPath := strings.Replace(file, "$", "%24", -1)
	if len(params) > 0 {
		iiifPath = singleJoiningSlash(iiifPath, params)
	}
	urlstring := singleJoiningSlash(ms.proxyURL, iiifPath)

	client := &http.Client{}
	log.Println("Proxy: ", urlstring)
	req2, err := http.NewRequest("GET", urlstring, nil)
	if err != nil {
		return err
	}

	token = "open"
	if storage.secret.Valid {
		sub := ms.subprefix + iiifPath
		token, err = NewJWT(storage.secret.String, sub, 7200)
		if err != nil {
			return err
		}
	}

	req2.Header.Add("X-Forwarded-Proto", "http")
	req2.Header.Add("X-Forwarded-Host", req.Host[:strings.IndexByte(req.Host, ':')])
	req2.Header.Add("X-Forwarded-Port", req.Host[(strings.IndexByte(req.Host, ':')+1):])
	req2.Header.Add("X-Forwarded-Path", singleJoiningSlash(ms.iiifAlias, token)+"/")
	req2.Header.Add("X-Forwarded-For", req.RemoteAddr[:strings.IndexByte(req.RemoteAddr, ':')])

/*
	for k, v := range req2.Header {
		log.Println("Key:", k, "Value:", v)
	}
*/	
	rs, err := client.Do(req2)
	if err != nil {
		return err
	}
	defer rs.Body.Close()

	io.Copy(writer, rs.Body)

	return nil
}

// query handler
func (ms *Mediaserver) Handler(writer http.ResponseWriter, req *http.Request, collection string, signature string, action string, params []string) (err error) {
	var (
		filebase     string
		path         string
		mimetype     string
		jwtkey       sql.NullString
		storageid    int
		paramstring  string
		naction      string
		nparamstring string
	)
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	sort.Strings(params)

	coll, err := ms.collections.ByName(collection)
	paramstring = strings.Trim(strings.Join(params, "/"), "/")

	naction = action
	nparamstring = paramstring
	isiiif := (action == "iiif")
	if isiiif {
		naction = "master"
		nparamstring = ""
	}

	row := ms.db.QueryRow("select filebase, path, mimetype, jwtkey, storageid FROM fullcache WHERE collection_id=? AND signature=? and action=? AND param=?", coll.id, signature, naction, nparamstring)
	if err != nil {
		log.Fatal(err)
	}
	err = row.Scan(&filebase, &path, &mimetype, &jwtkey, &storageid)
	if err != nil {
		log.Fatal(err)
		return err
	}
	if jwtkey.Valid {
		token, ok := req.URL.Query()["token"]
		if ok {
			sub := strings.ToLower(strings.TrimRight(ms.subprefix+strings.Trim(ms.alias, "/")+"/"+collection+"/"+signature+"/"+action+"/"+paramstring, "/"))
			ok, err := CheckJWT(token[0], jwtkey.String, sub)
			if !ok {
				writer.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(writer, "<html><body><h3>invalid access token: %s</h3></body></html>", err)
				return err
			}
		} else {
			writer.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(writer, "<html><body><h3>no access token: access denied</h3></body></html>")
			return err
		}
	}

	uri := strings.TrimRight(filebase, "/") + "/" + strings.TrimLeft(path, "/")
	URL, err := url.Parse(uri)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := os.Stat(URL.Path); err == nil {
		filePath := URL.Path
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

		if isiiif {
			iiifPath := strings.Replace(strings.Trim(strings.TrimPrefix(filePath, ms.iiifBase), "/"), "/", "%24", -1)
			if len(paramstring) > 0 {
				iiifPath = singleJoiningSlash(iiifPath, paramstring)
			}
			urlstring := singleJoiningSlash(ms.proxyURL, iiifPath)

			client := &http.Client{}
			log.Println("Proxy: ", urlstring)
			req2, err := http.NewRequest("GET", urlstring, nil)
			if err != nil {
				return err
			}

			token := "open"
			if jwtkey.Valid {
				secret := jwtkey.String
				sub := ms.subprefix + iiifPath
				token, err = NewJWT(secret, sub, 7200)
				if err != nil {
					return err
				}
			}

			req2.Header.Add("X-Forwarded-Proto", "http")
			req2.Header.Add("X-Forwarded-Host", req.Host[:strings.IndexByte(req.Host, ':')])
			req2.Header.Add("X-Forwarded-Port", req.Host[(strings.IndexByte(req.Host, ':')+1):])
			req2.Header.Add("X-Forwarded-Path", singleJoiningSlash(ms.iiifAlias, strconv.Itoa(storageid)+"_"+token)+"/")
			req2.Header.Add("X-Forwarded-For", req.RemoteAddr[:strings.IndexByte(req.RemoteAddr, ':')])
			for k, v := range req2.Header {
				log.Println("Key:", k, "Value:", v)
			}
			rs, err := client.Do(req2)
			if err != nil {
				return err
			}
			defer rs.Body.Close()

			io.Copy(writer, rs.Body)

			return nil
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
		http.ServeContent(writer, req, fileName, t, file)

		return nil
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
	parameters.Add("action", naction)
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
