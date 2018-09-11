package mediaserver

import (
	"database/sql"
	"errors"
	"log"
	"strconv"
	"strings"
)

// the collections
type Collections struct {
	db          *sql.DB
	collections map[string]Collection
}

type Collection struct {
	id   int
	name string
}

// Create a new Mediaserver
// db Database Handle
func NewCollections(db *sql.DB) *Collections {
	collections := &Collections{
		db: db,
	}
	collections.Init()
	return collections
}

// constructor
// load all collections into a map
func (colls *Collections) Init() (err error) {
	var (
		id   int
		name string
	)
	// initialize maps
	colls.collections = make(map[string]Collection)
	// get all collections
	rows, err := colls.db.Query("select collectionid as id, name from collection")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name)
		if err != nil {
			log.Fatal(err)
			break
		}
		// add collection to map
		colls.collections[strings.ToLower(name)] = Collection{name: name, id: id}
	}
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	return
}

func (colls *Collections) ByName(name string) (c Collection, err error) {
	c, ok := colls.collections[strings.ToLower(name)]
	if !ok {
		err = errors.New(name + " not found in collections")
	}
	return c, err
}

func (colls *Collections) ById(id int) (c Collection, err error) {
	for _, c = range colls.collections {
		if c.id == id {
			return
		}
	}
	err = errors.New(strconv.Itoa(id) + " not found in collections")
	return
}
