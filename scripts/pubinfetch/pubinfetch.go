package main

import (
	"flag"
	"log"

	lmdb "github.com/gwd/localmaildb/localmaildb"
	pisrc "github.com/gwd/localmaildb/pubinboxsrc"
)

var (
	mdbname = flag.String("mdb", "", "MailDB file")
	pipath  = flag.String("pipath", "", "Public Inbox path")
)

func main() {
	flag.Parse()

	if *mdbname == "" {
		log.Fatalf("Please specify a maildb file with -mdb")
	}
	if *pipath == "" {
		log.Fatalf("Please specify a public inbox path with -pipath")
	}

	// Attach a maildb
	mdb, err := lmdb.OpenMailDB(*mdbname)
	if err != nil {
		log.Fatalf("Opening temporary maildb file %s: %v", *mdbname, err)
	}

	src, err := pisrc.Connect(pisrc.PublicInboxInfo{Path: *pipath})
	if err != nil {
		log.Fatalf("Opening PublicInbox: %v", err)
	}

	log.Printf("Fetching mail")
	err = src.Fetch(mdb)
	if err != nil {
		log.Fatalf("Fetching messages: %v", err)
	}
}
