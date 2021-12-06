package entities

import (
	"os"
	"time"
)

type RunParams struct {
	KaraokeFolderPath string
	KaraokeDBPath     string
}

type KaraokeFileInfo struct {
	Id          int
	FolderPath  string
	FileName    string
	Artist      string
	Track       string
	Karaoketype string
}

type FileData struct {
	Path string
	Info os.FileInfo
}

type KaraokeQueue struct {
	id          int
	username    string
	filename    string
	folderpath  string
	karaoketype string
	date        time.Time
	playorder   int
	played      bool
}

type KaraokeFile struct {
	id          int
	filename    string
	folderpath  string
	karaoketype string
}

type KaraokeResult struct {
	Id       int
	Filename string
}
