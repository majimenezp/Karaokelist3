package dbaccess

import (
	"Karaokelist3/pkg/entities"
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type DBAccess interface {
}

type SqlliteAccess struct {
	dbpath string
	db     *sql.DB
}

func NewSqliteAccess(dbpath string) *SqlliteAccess {
	sa := SqlliteAccess{dbpath: dbpath}
	sa.InitDB()
	return &sa
}

func (sa *SqlliteAccess) InitDB() {
	currentDb, err := sql.Open("sqlite3", sa.dbpath)
	if err != nil {
		log.Fatal(err)
	}
	sa.db = currentDb
	sa.CreateKarokeFilesTable()
	sa.CreateKarokeQueueTable()
	//defer sa.db.Close()
}

func (sa *SqlliteAccess) CreateKarokeFilesTable() {
	stmt, err := sa.db.Prepare(`CREATE TABLE IF NOT EXISTS karaokefiles(
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
    filename    TEXT,
    folderpath TEXT,
    karaoketype STRING
	)`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = stmt.Exec()
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
}

func (sa *SqlliteAccess) CreateKarokeQueueTable() {
	stmt, err := sa.db.Prepare(`CREATE TABLE IF NOT EXISTS karaokequeue(
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
    username    TEXT,
    filename TEXT,
	folderpath TEXT,
    karaoketype STRING,
	date DATETIME,
	playorder INTEGER,
	played BOOLEAN
	)`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = stmt.Exec()
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
}

func (sa *SqlliteAccess) CleanKaraokeFileTable() {
	stmt, err := sa.db.Prepare(`DELETE FROM karaokefiles;UPDATE SQLITE_SEQUENCE SET SEQ=0 WHERE NAME='karaokefiles';`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = stmt.Exec()
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
}

func (sa *SqlliteAccess) InsertKaraokeFiles(data []entities.KaraokeFileInfo) {
	sqlStr := "INSERT INTO karaokefiles(filename, folderpath, karaoketype) VALUES "
	vals := []interface{}{}

	for _, row := range data {
		sqlStr += "(?, ?, ?),"
		vals = append(vals, row.FileName, row.FolderPath, row.Karaoketype)
	}
	//trim the last ,
	sqlStr = sqlStr[0 : len(sqlStr)-1]
	//prepare the statement
	stmt, err := sa.db.Prepare(sqlStr)
	if err != nil {
		log.Fatal(err)
	}
	//format all vals at once
	res, _ := stmt.Exec(vals...)
	log.Println(res)
}

func (sa *SqlliteAccess) GetKarokeFile() {

}

func (sa *SqlliteAccess) SearchKaraokeFiles(searchTerm string) []entities.KaraokeResult {
	rows, err := sa.db.Query("SELECT id,filename FROM karaokefiles WHERE filename like '%" + searchTerm + "%'")
	defer rows.Close()
	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	results := make([]entities.KaraokeResult, 0)
	for rows.Next() {
		row := entities.KaraokeResult{}
		err = rows.Scan(&row.Id, &row.Filename)
		if err != nil {
			log.Fatal(err)
		}
		results = append(results, row)
	}

	err = rows.Err()
	if err != nil {
		log.Fatal(err)
	}
	return results
}
