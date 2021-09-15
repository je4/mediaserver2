package mediaserver

import (
	"database/sql"
	"errors"
	"log"
	"net/url"
	"strconv"
	"sync"
)

// the storages
type Storages struct {
	db       *sql.DB
	storages map[int]Storage
	m        sync.RWMutex
}

type Storage struct {
	id       int
	name     string
	filebase string
	secret   sql.NullString
}

// Create a new Mediaserver
// db Database Handle
func NewStorages(db *sql.DB) *Storages {
	storages := &Storages{
		db: db,
	}
	storages.Init()
	return storages
}

// constructor
// load all collections into a map
func (stors *Storages) Init() (err error) {
	stors.m.Lock()
	defer stors.m.Unlock()
	var (
		id       int
		name     string
		filebase string
		secret   sql.NullString
	)
	// initialize maps
	stors.storages = make(map[int]Storage)
	// get all storages
	rows, err := stors.db.Query("select storageid as id, name, filebase, jwtkey from storage")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name, &filebase, &secret)
		if err != nil {
			log.Fatal(err)
			break
		}
		// add collection to map
		stors.storages[id] = Storage{name: name,
			id:       id,
			filebase: filebase,
			secret:   secret}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	return
}

func (stors *Storages) ById(id int) (s Storage, err error) {
	stors.m.RLock()
	defer stors.m.RUnlock()
	s, ok := stors.storages[id]
	if !ok {
		err = errors.New(strconv.Itoa(id) + " not found in storages")
	}
	return s, err
}

func (stors *Storages) ByName(name string) (s Storage, err error) {
	stors.m.RLock()
	defer stors.m.RUnlock()
	for _, s = range stors.storages {
		if s.name == name {
			return
		}
	}
	err = errors.New(name + " not found in collections")
	return
}

func (s *Storage) GetPath() (string, error) {
	_url, err := url.Parse(s.filebase)
	if err != nil {
		return "", err
	}
	return _url.Path, nil
}
