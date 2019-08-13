package main

import (
	lmdb "github.com/gwd/localmaildb"
	"github.com/spf13/viper"
	"log"
)

func main() {
	imapinfo := lmdb.ImapInfo{}

	viper.SetConfigName(".taskmail")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	if viper.IsSet("port") {
		imapinfo.Port = viper.GetInt("port")
	}

	if !viper.IsSet("imapserver") {
		log.Fatal("No imapserver configured")
	}
	imapinfo.Hostname = viper.GetString("imapserver")

	if !viper.IsSet("username") {
		log.Fatal("No username configured")
	}
	imapinfo.Username = viper.GetString("username")

	if !viper.IsSet("password") {
		log.Fatal("No password configured")
	}
	imapinfo.Password = viper.GetString("password")

	log.Println("Opening database")
	mdb, err := lmdb.OpenMailDB("maildb.sqlite", &imapinfo)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer mdb.Close()

	log.Println("Opening imap connection")
	if err = mdb.ImapConnect(); err != nil {
		log.Fatalf("Connecting to the IMAP server: %v", err)
	}

	if err = mdb.Fetch(); err != nil {
		log.Fatalf("Fetching mail: %v", err)
	}
}
