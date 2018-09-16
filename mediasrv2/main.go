package main

import (
	"config"
	"database/sql"
	"fmt"

	"digma/mediaserver"
	"flag"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"net/http"

	"github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
	accesslog "github.com/mash/go-accesslog"
)

/*
 * my first go program...
 *
 */

var (
	VERSION    string = "xerver/v3.0"
	FCGI_PROTO string = "unix"
	FCGI_ADDR  string = ""
)

type logger struct {
}

func (l logger) Log(record accesslog.LogRecord) {
	log.Println(record.Host + " \"" + record.Method + " " + record.Uri + " " + record.Protocol + "\" " + strconv.Itoa(record.Status) + " " + strconv.FormatInt(record.Size, 10))
}

func main() {
	
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	
	// get location of config file
	cfgfile := flag.String("cfg", "/etc/mediasrv2.toml", "location of config file")
	flag.Parse()
	cfg := config.Load(*cfgfile)

	// get database connection handle
	db, err := sql.Open(cfg.Mediaserver.DB.ServerType, cfg.Mediaserver.DB.DSN)
	if err != nil {
		panic(err.Error()) // Just for example purpose. You should use proper error handling instead of panic
	}
	defer db.Close()

	// Open doesn't open a connection. Validate DSN data:
	err = db.Ping()
	if err != nil {
		panic(err.Error()) // proper error handling instead of panic in your app
	}

	// create a new router
	router := httprouter.New()

	// iterate through folders...
	for folderName, folder := range cfg.Folders {
		folder := folder
		log.Printf("Folder[%s] %s on %s as %s\n", folderName, folder.Title, folder.Path, folder.Alias)

		// add the filesystem reader to the router
		router.GET(strings.TrimRight(folder.Alias, "/")+"/*path", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
			AuthFileSrvHandler(writer, req, folder.Secret, strings.TrimRight(folder.Path, "/"), folder.Title, params)
		})
	}
	// create mediaserver route
	ms := mediaserver.New(db, cfg.Mediaserver.FCGI.Proto, cfg.Mediaserver.FCGI.Addr, cfg.Mediaserver.FCGI.Script)

	// route with parameters
	router.GET(strings.TrimRight(cfg.Mediaserver.Alias, "/")+"/:collection/:signature/:action/*params", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		collection := params.ByName("collection")
		signature := params.ByName("signature")
		action := params.ByName("action")
		paramString := params.ByName("params")
		ps := strings.Split(paramString, "/")
		ms.Handler(writer, req, collection, signature, action, ps)
	})
	
	// route without parameters
	router.GET(strings.TrimRight(cfg.Mediaserver.Alias, "/")+"/:collection/:signature/:action", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		collection := params.ByName("collection")
		signature := params.ByName("signature")
		action := params.ByName("action")
		paramString := ""
		ps := strings.Split(paramString, "/")
		ms.Handler(writer, req, collection, signature, action, ps)
	})
	addr := cfg.IP + ":" + strconv.Itoa(cfg.Port)
	log.Printf("Starting HTTP server on %q", addr)
	go func() {
		l := logger{}
		log.Fatal(http.ListenAndServe(addr, accesslog.NewLoggingHandler(router, l)))
	}()

	select {} // wait forever
}

func AuthFileSrvHandler(w http.ResponseWriter, r *http.Request, jwtSecret string, basePath string, title string, params httprouter.Params) {

	//log.Println(params)
	if jwtSecret != "" {
		auth, ok := r.URL.Query()["auth"]
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "<html><body style='font-size:100px'>Zugriff verweigert, Schlüssel fehlt</body></html>")
			return
		}
		tokenstring := auth[0]

		token, _ := jwt.Parse(tokenstring, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})
		if !token.Valid {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "<html><body style='font-size:100px'>Zugriff verweigert, Schlüsselfehler</body></html>")
			return
		}
	}

	filePath := basePath + "/" + params.ByName("path")
	_, fileName := path.Split(filePath)

	fileStat, err := os.Stat(filePath)
	if err != nil {
		fmt.Println(err)
		return
	}

	if fileStat.IsDir() {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, "<html><body style='font-size:100px'>Zugriff auf Verzeichnis %s verweigert</body></html>", fileName)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("%s not found\n", filePath)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "<html><body style='font-size:100px'>Die Kollektion %s enthält keine Datei %s</body></html>", title, fileName)
		return
	}
	defer file.Close()

	t := fileStat.ModTime()
	w.Header().Set("Server", "DIGMA Mediaserver")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, fileName, t, file)
}
