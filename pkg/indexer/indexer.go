package indexer

import (
	"Karaokelist3/pkg/entities"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type Indexer interface {
	ScanFiles(folderpath string) error
	GetTrackInfo(filename string) (string, string)
}

type CDGIndexer struct {
	files        []entities.FileData
	KaraokeFiles []entities.KaraokeFileInfo
}

func (i *CDGIndexer) ScanFiles(folderpath string) error {
	i.files = []entities.FileData{}
	log.Printf("Starting the file indexing...")
	err := filepath.Walk(folderpath, i.FileWalk)
	if err != nil {
		log.Println(err)
	}
	i.KaraokeFiles = []entities.KaraokeFileInfo{}
	for a := 0; a < len(i.files); a++ {
		log.Printf(i.files[a].Path)
		log.Printf(i.files[a].Info.Name())
		if a+1 < len(i.files) {
			currentExt := filepath.Ext(i.files[a].Info.Name())
			nextExt := filepath.Ext(i.files[a+1].Info.Name())
			if strings.Replace(strings.ToLower(i.files[a].Info.Name()), currentExt, "", -1) == strings.Replace(strings.ToLower(i.files[a+1].Info.Name()), nextExt, "", -1) {
				newKaraokeFile := entities.KaraokeFileInfo{}
				newKaraokeFile.Karaoketype = "CDG"
				newKaraokeFile.FileName = strings.Replace(strings.ToLower(i.files[a].Info.Name()), currentExt, "", -1)
				newKaraokeFile.FolderPath = filepath.Dir(i.files[a].Path)
				//newKaraokeFile.Artist, newKaraokeFile.Track = i.GetTrackInfo(newKaraokeFile.FileName)
				i.KaraokeFiles = append(i.KaraokeFiles, newKaraokeFile)
				a += 1
			}
		}
	}
	return nil
}

func (i *CDGIndexer) GetTrackInfo(filename string) (string, string) {

	return "", ""
}

func (i *CDGIndexer) FileWalk(path string, info os.FileInfo, err error) error {
	if !info.IsDir() {
		i.files = append(i.files, entities.FileData{Path: path, Info: info})
	}
	if err != nil {
		return err
	}
	fmt.Println(path, info.Size())
	return nil
}
