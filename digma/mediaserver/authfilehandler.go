package mediaserver

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func AuthFileSrvHandler(w http.ResponseWriter, r *http.Request, jwtSecret string, subPrefix string, basePath string, alias string, params httprouter.Params) {

	//log.Println(params)
	if jwtSecret != "" {
		token, ok := r.URL.Query()["token"]
		if ok {
			ok, err := CheckJWT(token[0], jwtSecret, subPrefix+strings.ToLower(strings.Trim(alias, "/")+"/"+strings.TrimLeft(params.ByName("path"), "/")))
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, "<html><body style='font-size:100px'>invalid access token: %s</body></html>", err)
				return
			}
		} else {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "<html><body style='font-size:100px'>no access token: access denied</body></html>")
			return
		}
	}

	filePath := strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(params.ByName("path"), "/")
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
		fmt.Fprintf(w, "<html><body style='font-size:100px'>Die Kollektion %s enthält keine Datei %s</body></html>", alias, fileName)
		return
	}
	defer file.Close()

	t := fileStat.ModTime()
	w.Header().Set("Server", "DIGMA Mediaserver")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, fileName, t, file)
}
