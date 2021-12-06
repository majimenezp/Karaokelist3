package cmd

import (
	"Karaokelist3/pkg/entities"
	"Karaokelist3/pkg/player"
	"log"
	"os"
	"path"

	"github.com/spf13/cobra"
)

var playerParams = entities.RunParams{}
var karaokePlayerCmd = &cobra.Command{
	Use:   "player",
	Short: "start karaoke player",
	Long:  `start karaoke player`,
	Run: func(cmd *cobra.Command, args []string) {
		startplayer(playerParams)
	},
}

func init() {
	rootCmd.AddCommand(karaokePlayerCmd)
	karaokePlayerCmd.Flags().StringVarP(&playerParams.KaraokeFolderPath, "karaoke-folder", "f", "", "Folder path where karaoke files are located")
	err := karaokePlayerCmd.MarkFlagRequired("karaoke-folder")
	handleRequiredFlagErrors(err)
}

func startplayer(playerParams entities.RunParams) {
	currentPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	playerParams.KaraokeDBPath = path.Join(currentPath, "karaoke.sqlite")
	player.StartPlayer(playerParams)
}
