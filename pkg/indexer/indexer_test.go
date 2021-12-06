package indexer

import (
	"Karaokelist3/pkg/dbaccess"
	"log"
	"testing"
)

func TestCDGIndex(t *testing.T) {
	indexer := CDGIndexer{}
	error := indexer.ScanFiles("/mnt/f/testkaraoke/")
	if error != nil {
		t.Error(error)
	}
	log.Printf("Number of files: %d", len(indexer.KaraokeFiles))
	sqliteaccess := dbaccess.NewSqliteAccess("/mnt/f/testkaraoke/test.db")
	sqliteaccess.CleanKaraokeFileTable()
	sqliteaccess.InsertKaraokeFiles(indexer.KaraokeFiles)

}

func TestSearchSong(t *testing.T) {
	sqliteaccess := dbaccess.NewSqliteAccess("/mnt/f/testkaraoke/test.db")
	results := sqliteaccess.SearchKaraokeFiles("la chica de humo")
	if len(results) == 0 {
		t.Error()
	}
	log.Printf("Number of results: %d", len(results))
}
