package config

import (
	"log"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Folders map[string]Folder
	Mediaserver Mediaserver 
	Port    int
	IP      string
	TLS     bool
	TLSCert string
	TLSKey  string
}

type Mediaserver struct {
	DB      database `toml:"database"`
	FCGI	fcgi `toml:"fcgi"`
	Alias string
}

type fcgi struct {
	Proto string
	Addr string
	Script string
}

type database struct {
	ServerType string
	DSN        string
	ConnMax    int `toml:"connection_max"`
}

type Folder struct {
	Title  string
	Path   string
	Secret string
	Alias  string
}

func Load(filepath string) Config {
	var conf Config
	_, err := toml.DecodeFile(filepath, &conf)
	if err != nil {
		log.Fatalln("Error on loading config: ", err)
	}
	return conf
}
