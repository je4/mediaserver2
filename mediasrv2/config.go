package main

import (
	"log"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Folders     map[string]Folder
	Mediaserver Mediaserver
	Port        int
	IP          string
	TLS         bool
	TLSCert     string
	TLSKey      string
	SubPrefix   string
	Logfile     string
}

type Mediaserver struct {
	DB    database `toml:"database"`
	FCGI  fcgi     `toml:"fcgi"`
	IIIF  iiif     `toml:"iiif"`
	Alias string
}

type fcgi struct {
	Proto  string
	Addr   string
	Script string
}

type iiif struct {
	URL      string
	IIIFBase string
	Alias    string
}

type database struct {
	ServerType string
	DSN        string
	ConnMax    int `toml:"connection_max"`
}

type Folder struct {
	Title   string
	Path    string
	Secret  string
	Alias   string
	Subnets []Subnet
}

type Subnet struct {
	net string
}

func Load(filepath string) Config {
	var conf Config
	_, err := toml.DecodeFile(filepath, &conf)
	if err != nil {
		log.Fatalln("Error on loading config: ", err)
	}
	return conf
}
