package mediaserver

import (
	"database/sql"
	"errors"
	"log"
	"strconv"
)

// the storages
type Storages struct {
	db       *sql.DB
	storages map[int]Storage
}

type Storage struct {
	id       int
	name     string
	filebase string
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
	var (
		id       int
		name     string
		filebase string
	)
	// initialize maps
	stors.storages = make(map[int]Storage)
	// get all storages
	rows, err := stors.db.Query("select storageid as id, name, filebase from storage")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name, &filebase)
		if err != nil {
			log.Fatal(err)
			break
		}
		// add collection to map
		stors.storages[id] = Storage{name: name, id: id, filebase: filebase}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	return
}

func (stors *Storages) ById(id int) (s Storage, err error) {
	s, ok := stors.storages[id]
	if !ok {
		err = errors.New(strconv.Itoa(id) + " not found in storages")
	}
	return s, err
}

func (stors *Storages) ByName(name string) (s Storage, err error) {
	for _, s = range stors.storages {
		if s.name == name {
			return
		}
	}
	err = errors.New(name + " not found in collections")
	return
}
