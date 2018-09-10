package main

import (
	"config"
	"database/sql"
	"digma/mediaserver"
	"flag"
	"log"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	//"net/http"
	"github.com/qiangxue/fasthttp-routing"
	"github.com/valyala/fasthttp"
	//"github.com/valyala/fasthttp/expvarhandler"
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

func main() {
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
	router := routing.New()

	// iterate through folders...
	for folderName, folder := range cfg.Folders {
		folder := folder
		log.Printf("Folder[%s] %s on %s as %s\n", folderName, folder.Title, folder.Path, folder.Alias)
		// create a filesystem reader for every folder
		fs := &fasthttp.FS{
			Root:               folder.Path,
			PathRewrite:        fasthttp.NewPathPrefixStripper(len(strings.TrimRight(folder.Alias, "/"))),
			IndexNames:         []string{"index.html"},
			GenerateIndexPages: false,
			Compress:           false,
			AcceptByteRange:    true,
		}
		fsHandler := fs.NewRequestHandler()

		// add the filesystem reader to the router
		router.Get(strings.TrimRight(folder.Alias, "/")+"/*", func(c *routing.Context) error {
			fsHandler(c.RequestCtx)
			return nil
		})
	}

	// create mediaserver route
	ms := mediaserver.New(db, cfg.Mediaserver.FCGI.Proto, cfg.Mediaserver.FCGI.Addr, cfg.Mediaserver.FCGI.Script)

	router.Get(strings.TrimRight(cfg.Mediaserver.Alias, "/")+"/<collection>/<signature>/<function>/<params:.*>", func(c *routing.Context) error {
		collection := c.Param("collection")
		signature := c.Param("signature")
		function := c.Param("function")
		paramString := c.Param("params")
		params := strings.Split(paramString, "/")
		ms.Handler(c.RequestCtx, collection, signature, function, params)
		return nil
	})

	addr := cfg.IP + ":" + strconv.Itoa(cfg.Port)
	log.Printf("Starting HTTP server on %q", addr)
	go func() {
		if err := fasthttp.ListenAndServe(addr, router.HandleRequest); err != nil {
			log.Fatalf("error in ListenAndServe: %s", err)
		}
	}()

	select {} // wait forever
}
