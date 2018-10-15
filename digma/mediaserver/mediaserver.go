package mediaserver

import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	logging "github.com/op/go-logging"
	"github.com/tomasen/fcgi_client"
)

var (
	VERSION = "DIGMA Mediaserver 0.1"
)

// the Mediaserver does some nice conversion things for various media types
// initially uses some php stuff to do it...
type Mediaserver struct {
	db          *sql.DB
	cfg         *Config
	collections *Collections
	storages    *Storages
	logger      *logging.Logger
}

// Create a new Mediaserver
// db Database Handle
// fcgiProto Protocol for FCGI connection
// fcgiAddr Address for FCGI connection
func New(db *sql.DB, cfg *Config, logger *logging.Logger) *Mediaserver {
	mediaserver := &Mediaserver{
		db:     db,
		cfg:    cfg,
		logger: logger}
	mediaserver.Init()
	return mediaserver
}

// constructor
func (ms *Mediaserver) Init() (err error) {
	ms.collections = NewCollections(ms.db)
	ms.storages = NewStorages(ms.db)
	return
}

// html output of error message
func (ms *Mediaserver) DoPanic(writer http.ResponseWriter, req *http.Request, status int, message string) (err error) {
	type errData struct {
		Status     int
		StatusText string
		Message    string
	}

	ms.logger.Error(message)
	data := errData{
		Status:     status,
		StatusText: http.StatusText(status),
		Message:    message,
	}
	writer.WriteHeader(status)
	t := template.Must(template.ParseFiles(ms.cfg.ErrorTemplate))
	t.Execute(writer, data)
	return
}

// helper to determine protocol, host and port. uses some heuristics
func (ms *Mediaserver) getProtoHostPort(req *http.Request) (proto string, host string, port int) {
	var err error
	host = req.Host
	port = ms.cfg.Port
	// is there a colon are some characters left behind?
	if p := strings.IndexByte(req.Host, ':'); p > 0 && len(req.Host) > p {
		host = req.Host[:p]
		port, err = strconv.Atoi(req.Host[(p + 1):])
		if err != nil {
			port = ms.cfg.Port
		}
	}
	proto = "http"
	if req.TLS != nil {
		proto = "https"
	}
	return
}

// IIIF handler
func (ms *Mediaserver) HandlerIIIF(writer http.ResponseWriter, req *http.Request, file string, params string, token string) (err error) {
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	// token format: <storageid>_<token>
	tokenParts := strings.SplitN(token, "_", 2)
	storageid, err := strconv.Atoi(tokenParts[0])
	if err != nil {
		ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("Invalid token - no storageid: %s", token))
		return err
	}
	storage, err := ms.storages.ById(storageid)
	if err != nil {
		ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("Invalid token - storage #%d not found: %s", storageid, token))
		return err
	}
	filename := singleJoiningSlash(ms.cfg.Mediaserver.IIIF.IIIFBase, strings.Replace(file, "$", "/", -1))
	storagePath, err := storage.GetPath()
	if !strings.HasPrefix(filename, storagePath) {
		ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("Invalid storage #%d for file %s - %s", storageid, filename, storagePath))
		return err
	}
	if storage.secret.Valid {
		token := tokenParts[1]
		sub := strings.Replace(strings.ToLower(strings.TrimRight(ms.cfg.SubPrefix+file, "/")), "$", "%24", -1)
		err := CheckJWT(token, storage.secret.String, sub)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusForbidden, err.Error())
			return err
		}
	}
	iiifPath := strings.Replace(file, "$", "%24", -1)
	filePath := iiifPath
	if len(params) > 0 {
		iiifPath = singleJoiningSlash(iiifPath, params)
	}
	urlstring := singleJoiningSlash(ms.cfg.Mediaserver.IIIF.URL, iiifPath)

	client := &http.Client{}
	log.Println("Proxy: ", urlstring)
	req2, err := http.NewRequest("GET", urlstring, nil)
	if err != nil {
		ms.DoPanic(writer, req, http.StatusInternalServerError, fmt.Sprintf("Error creating new request: %s", err.Error()))
		return err
	}

	token = "open"
	if storage.secret.Valid {
		sub := ms.cfg.SubPrefix + filePath
		token, err = NewJWT(storage.secret.String, sub, 7200)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusInternalServerError, fmt.Sprintf("Error creating access token: %s", err.Error()))
			return err
		}
	}
	token = strconv.Itoa(storageid) + "_" + token
	proto, host, port := ms.getProtoHostPort(req)
	req2.Header.Add("X-Forwarded-Proto", proto)
	req2.Header.Add("X-Forwarded-Host", host)
	req2.Header.Add("X-Forwarded-Port", strconv.Itoa(port))
	req2.Header.Add("X-Forwarded-Path", singleJoiningSlash(ms.cfg.Mediaserver.IIIF.Alias, token)+"/")
	req2.Header.Add("X-Forwarded-For", req.RemoteAddr[:strings.IndexByte(req.RemoteAddr, ':')])

	rs, err := client.Do(req2)
	if err != nil {
		ms.DoPanic(writer, req, http.StatusBadGateway, fmt.Sprintf("Error calling proxy: %s - %s", urlstring, err))
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
		token        []string
		ok           bool = false
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
	ms.logger.Debug("QUERY: /" + collection + "[" + strconv.Itoa(coll.id) + "]/" + signature + "/" + naction + "/" + nparamstring)

	found := true
	row := ms.db.QueryRow("select filebase, path, mimetype, jwtkey, storageid FROM fullcache WHERE collection_id=? AND signature=? and action=? AND param=?", coll.id, signature, naction, nparamstring)
	if err != nil {
		ms.DoPanic(writer, req, http.StatusNotFound, fmt.Sprintf("could not query %s[%d]/%s/%s/%s", collection, coll.id, signature, naction, nparamstring))
		return err
	}
	err = row.Scan(&filebase, &path, &mimetype, &jwtkey, &storageid)
	if err != nil {
		found = false
		ms.logger.Debug(fmt.Sprintf("could not find in databbase [%s/%s/%s/%s]", collection, signature, naction, nparamstring))
		//		ms.DoPanic(writer, req, http.StatusNotFound, fmt.Sprintf("could not find %s[%d]/%s/%s/%s", collection, coll.id, signature, naction, nparamstring))
		//		return nil
	}
	if( found && isiiif && !(mimetype == "image/png" || mimetype == "image/tiff" )) {
		ms.logger.Debug("Mimetype: "+ mimetype )
				
		naction = "convert"
		nparamstring = "formatpng"
		row := ms.db.QueryRow("select filebase, path, jwtkey, storageid FROM fullcache WHERE collection_id=? AND signature=? and action=? AND param=?", coll.id, signature, naction, nparamstring)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusNotFound, fmt.Sprintf("could not query %s[%d]/%s/%s/%s", collection, coll.id, signature, naction, nparamstring))
			return err
		}
		err = row.Scan(&filebase, &path, &jwtkey, &storageid)
		if err != nil {
			found = false
		ms.logger.Debug(fmt.Sprintf("could not find in databbase [%s/%s/%s/%s]", collection, signature, naction, nparamstring))
			//		ms.DoPanic(writer, req, http.StatusNotFound, fmt.Sprintf("could not find %s[%d]/%s/%s/%s", collection, coll.id, signature, naction, nparamstring))
			//		return nil
		}
	}
	// get token from uri parameter
	token, ok = req.URL.Query()["token"]
	// sometimes auth is used instead of token...
	if !ok {
		token, ok = req.URL.Query()["auth"]
	}
	if found && jwtkey.Valid {
		if ok {
			sub := strings.ToLower(strings.TrimRight(ms.cfg.SubPrefix+collection+"/"+signature+"/"+action+"/"+paramstring, "/"))
			err := CheckJWT(token[0], jwtkey.String, sub)
			if err != nil {
				ms.DoPanic(writer, req, http.StatusForbidden, err.Error())
				return err
			}
		} else {
			ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("no access token"))
			return err
		}
	}

	// if not found, then forward to php mediaserver
	if !found {
		ms.logger.Debugf("Start fcgi %s %s", ms.cfg.Mediaserver.FCGI.Proto, ms.cfg.Mediaserver.FCGI.Addr)
		fcgi, err := fcgiclient.Dial(ms.cfg.Mediaserver.FCGI.Proto, ms.cfg.Mediaserver.FCGI.Addr)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusBadGateway, fmt.Sprintf("Unable to connect to fcgi backend: %s://%s - %s", ms.cfg.Mediaserver.FCGI.Proto, ms.cfg.Mediaserver.FCGI.Addr, err))
			//ctx.Error("Unable to connect to the backend", 502)
			return err
		}
		parameters := url.Values{}
		parameters.Add("collection", collection)
		parameters.Add("signature", signature)
		parameters.Add("action", naction)
		if len(token) > 0 {
			parameters.Add("token", token[0])
		}
		for _, value := range params {
			if value != "" {
				parameters.Add("params[]", value)
			}
		}

		env := map[string]string{
			"AUTH_TYPE":       "", // Not used
			"SCRIPT_FILENAME": ms.cfg.Mediaserver.FCGI.Script,
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
			ms.DoPanic(writer, req, http.StatusBadGateway, fmt.Sprintf("Unable to get data from fcgi backend: %s:%s - %s", ms.cfg.Mediaserver.FCGI.Proto, ms.cfg.Mediaserver.FCGI.Addr, err))
			return err
		}
		contentType := resp.Header.Get("Content-type")
		if contentType == "" {
			contentType = "text/html"
		}
		writer.Header().Set("Content-type", contentType)

		_, err = io.Copy(writer, resp.Body)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusBadGateway, fmt.Sprintf("Unable to copy content from fcgi backend: %s://%s - %s", ms.cfg.Mediaserver.FCGI.Proto, ms.cfg.Mediaserver.FCGI.Addr, err))
			return err
		}

		return nil
	}

	uri := strings.TrimRight(filebase, "/") + "/" + strings.TrimLeft(path, "/")
	URL, err := url.Parse(uri)
	if err != nil {
		ms.logger.Critical(err)
	}
	if _, err := os.Stat(URL.Path); err == nil {
		filePath := URL.Path
		_, fileName := filepath.Split(filePath)

		fileStat, err := os.Stat(filePath)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("Cannot stat file: %s - %s", fileName, err.Error()))
			return err
		}

		if fileStat.IsDir() {
			ms.DoPanic(writer, req, http.StatusForbidden, fmt.Sprintf("Access to folder %s denied", fileName))
			return err
		}

		if isiiif {
			iiifPath := strings.Replace(strings.Trim(strings.TrimPrefix(filePath, ms.cfg.Mediaserver.IIIF.IIIFBase), "/"), "/", "%24", -1)
			iiifPathWithParam := iiifPath
			if len(paramstring) > 0 {
				iiifPathWithParam = singleJoiningSlash(iiifPath, paramstring)
			}
			urlstring := singleJoiningSlash(ms.cfg.Mediaserver.IIIF.URL, iiifPathWithParam)

			client := &http.Client{}
			ms.logger.Debugf("Proxy: %s", urlstring)
			req2, err := http.NewRequest("GET", urlstring, nil)
			if err != nil {
				ms.DoPanic(writer, req, http.StatusInternalServerError, fmt.Sprintf("Error creating http request for %s: %s", urlstring, err))
				return err
			}

			token := "open"
			if jwtkey.Valid {
				secret := jwtkey.String
				sub := ms.cfg.SubPrefix + iiifPath
				token, err = NewJWT(secret, sub, 7200)
				if err != nil {
					ms.DoPanic(writer, req, http.StatusInternalServerError, fmt.Sprintf("Error creating access token for %s: %s", sub, err))
					return err
				}
			}

			//ms.logger.Debug("req.Host:", req.Host)
			proto, host, port := ms.getProtoHostPort(req)
			req2.Header.Add("X-Forwarded-Proto", proto)
			req2.Header.Add("X-Forwarded-Host", host)
			req2.Header.Add("X-Forwarded-Port", strconv.Itoa(port))
			req2.Header.Add("X-Forwarded-Path", singleJoiningSlash(ms.cfg.Mediaserver.IIIF.Alias, strconv.Itoa(storageid)+"_"+token)+"/")
			req2.Header.Add("X-Forwarded-For", req.RemoteAddr[:strings.IndexByte(req.RemoteAddr, ':')])
			/*
				for k, v := range req2.Header {
					log.Println("Key:", k, "Value:", v)
				}
			*/
			rs, err := client.Do(req2)
			if err != nil {
				ms.DoPanic(writer, req, http.StatusBadGateway, fmt.Sprintf("Error calling proxy: %s - %s", urlstring, err))
				return err
			}
			defer rs.Body.Close()

			io.Copy(writer, rs.Body)

			return nil
		}

		file, err := os.Open(filePath)
		if err != nil {
			ms.DoPanic(writer, req, http.StatusNotFound, fmt.Sprintf("File not found: %s - %s", fileName, err.Error()))
			return err
		}
		defer file.Close()

		t := fileStat.ModTime()
		writer.Header().Set("Server", VERSION)
		//		log.Println("serve: ", fileName)
		http.ServeContent(writer, req, fileName, t, file)

		return nil
	}
	return nil
}
