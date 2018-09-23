package mediaserver

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func (ms *Mediaserver) AuthFileSrvHandler(w http.ResponseWriter, req *http.Request, jwtSecret string, subPrefix string, basePath string, alias string, params httprouter.Params) {

	//log.Println(params)
	if jwtSecret != "" {
		token, ok := req.URL.Query()["token"]
		if !ok {
			token, ok = req.URL.Query()["auth"]
		}
		if ok {
			sub := subPrefix + req.URL.EscapedPath() // strings.ToLower(strings.Trim(alias, "/")+"/"+strings.TrimLeft(params.ByName("path"), "/"))
			err := CheckJWT(token[0], jwtSecret, sub)
			if err != nil {
				ms.DoPanic(w, req, http.StatusForbidden, err.Error())
				return
			}
		} else {
			ms.DoPanic(w, req, http.StatusForbidden, fmt.Sprintf("no access token"))
			return
		}
	}

	filePath := strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(params.ByName("path"), "/")
	_, fileName := path.Split(filePath)

	fileStat, err := os.Stat(filePath)
	if err != nil {
		ms.DoPanic(w, req, http.StatusNotFound, err.Error())
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
		fmt.Fprintf(w, "<html><body style='font-size:100px'>Die Kollektion %s enthält keine Datei %s</body></html>", alias, fileName)
		return
	}
	defer file.Close()

	t := fileStat.ModTime()
	w.Header().Set("Server", "DIGMA Mediaserver")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, req, fileName, t, file)
}
