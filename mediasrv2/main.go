package main

import (
	"database/sql"
	"digma/mediaserver"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"net/http"

	"github.com/julienschmidt/httprouter"
	accesslog "github.com/mash/go-accesslog"
	"github.com/op/go-logging"
)

/*
 * my first go program...
 *
 */

var (
	VERSION    string = "DIGMA Mediaserver 0.1"
	FCGI_PROTO string = "unix"
	FCGI_ADDR  string = ""
)

type logger struct {
	handle *os.File
}

var _log = logging.MustGetLogger("mediaserver2")
var _logformat = logging.MustStringFormatter(
	`%{time:2006-01-02T15:04:05.000} %{shortfunc} > %{level:.5s} - %{message}`,
)

func (l logger) Log(record accesslog.LogRecord) {
	//log.Println(record.Host+" ["+(time.Now().Format(time.RFC3339))+"] \""+record.Method+" "+record.Uri+" "+record.Protocol+"\" "+strconv.Itoa(record.Status)+" "+strconv.FormatInt(record.Size, 10))
	fmt.Fprintf(l.handle, "%s [%s] \"%s %s %s\" %d %d\n", record.Host, time.Now().Format(time.RFC3339), record.Method, record.Uri, record.Protocol, record.Status, record.Size)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// get location of config file
	cfgfile := flag.String("cfg", "/etc/mediasrv2.toml", "location of config file")
	flag.Parse()
	cfg := mediaserver.LoadConfig(*cfgfile)

	lf, err := os.OpenFile(cfg.Logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer lf.Close()

	backend := logging.NewLogBackend(lf, "", 0)

	backendLeveled := logging.AddModuleLevel(backend)
	switch cfg.Loglevel {
	case "critical":
		backendLeveled.SetLevel(logging.CRITICAL, "")
	case "warn":
		backendLeveled.SetLevel(logging.WARNING, "")
	case "notice":
		backendLeveled.SetLevel(logging.NOTICE, "")
	case "info":
		backendLeveled.SetLevel(logging.INFO, "")
	case "debug":
		backendLeveled.SetLevel(logging.DEBUG, "")
	default:
		backendLeveled.SetLevel(logging.ERROR, "")

	}

	logging.SetFormatter(_logformat)
	logging.SetBackend(backendLeveled)

	//log.SetOutput(lf)

	// get database connection handle
	db, err := sql.Open(cfg.Mediaserver.DB.ServerType, cfg.Mediaserver.DB.DSN)
	if err != nil {
		panic(err.Error())
	}
	defer db.Close()

	// Open doesn't open a connection. Validate DSN data:
	err = db.Ping()
	if err != nil {
		panic(err.Error())
	}

	// create mediaserver route
	ms := mediaserver.New(db, &cfg, _log)

	// create a new router
	router := httprouter.New()

	// iterate through folders...
	for folderName, folder := range cfg.Folders {
		folder := folder
		log.Printf("Folder[%s] %s on %s as %s\n", folderName, folder.Title, folder.Path, folder.Alias)

		// add the filesystem reader to the router
		router.GET(strings.TrimRight(folder.Alias, "/")+"/*path", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
			writer.Header().Set("Server", VERSION)
			writer.Header().Set("Access-Control-Allow-Origin", "*")
			ms.AuthFileSrvHandler(writer, req, folder.Secret, cfg.SubPrefix, strings.TrimRight(folder.Path, "/"), folder.Alias, params)
		})
	}

	// route with parameters
	router.GET(strings.TrimRight(cfg.Mediaserver.Alias, "/")+"/:collection/:signature/:action/*params", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		collection := params.ByName("collection")
		signature := params.ByName("signature")
		action := params.ByName("action")
		paramString := strings.ToLower(params.ByName("params"))
		ps := strings.Split(paramString, "/")
		writer.Header().Set("Server", VERSION)
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		ms.Handler(writer, req, collection, signature, action, ps)
	})

	// route without parameters
	router.GET(strings.TrimRight(cfg.Mediaserver.Alias, "/")+"/:collection/:signature/:action", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		collection := params.ByName("collection")
		signature := params.ByName("signature")
		action := params.ByName("action")
		paramString := ""
		ps := strings.Split(paramString, "/")
		writer.Header().Set("Server", VERSION)
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		ms.Handler(writer, req, collection, signature, action, ps)
	})

	// route for IIIF
	router.GET(strings.TrimRight(cfg.Mediaserver.IIIF.Alias, "/")+"/:token/:service/:api/:file/*params", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		file := params.ByName("file")
		token := params.ByName("token")
		paramString := params.ByName("params")
		writer.Header().Set("Server", VERSION)
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		ms.HandlerIIIF(writer, req, file, paramString, token)
	})
 
	// route for IIIF without parameters
	router.GET(strings.TrimRight(cfg.Mediaserver.IIIF.Alias, "/")+"/:token/:service/:api/:file", func(writer http.ResponseWriter, req *http.Request, params httprouter.Params) {
		file := params.ByName("file")
		token := params.ByName("token")
		writer.Header().Set("Server", VERSION)
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		ms.HandlerIIIF(writer, req, file, "", token)
	})

	addr := cfg.IP + ":" + strconv.Itoa(cfg.Port)
	_log.Info("Starting HTTP server on", addr)

	go func() {
		f, err := os.OpenFile(cfg.Accesslog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		l := logger{handle: f}

		if cfg.TLS {
			log.Fatal(http.ListenAndServeTLS(addr, cfg.TLSCert, cfg.TLSKey, accesslog.NewLoggingHandler(router, l)))
		} else {
			log.Fatal(http.ListenAndServe(addr, accesslog.NewLoggingHandler(router, l)))
		}
	}()

	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	sig := <-gracefulStop
	fmt.Printf("caught sig: %+v\n", sig)
	fmt.Println("Wait for 2 second to finish processing")
	time.Sleep(2 * time.Second)
	os.Exit(0)
}
