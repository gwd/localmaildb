package main

import (
	lmdb "github.com/gwd/localmaildb"
	"github.com/spf13/viper"
	"log"
)

func TreePrint(message *lmdb.MessageTree, indent string) {
	log.Printf("%s %v %s", indent, message.Envelope.Date, message.Envelope.Subject)
	for _, reply := range message.Replies {
		TreePrint(reply, indent+"*")
	}
}

func main() {
	mailbox := lmdb.MailboxInfo{}

	viper.SetConfigName(".taskmail")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	if !viper.IsSet("mailboxname") {
		log.Fatal("No mailbox name")
	}
	mailbox.MailboxName = viper.GetString("mailboxname")

	if viper.IsSet("port") {
		mailbox.Port = viper.GetInt("port")
	}

	if !viper.IsSet("imapserver") {
		log.Fatal("No imapserver configured")
	}
	mailbox.Hostname = viper.GetString("imapserver")

	if !viper.IsSet("username") {
		log.Fatal("No username configured")
	}
	mailbox.Username = viper.GetString("username")

	if !viper.IsSet("password") {
		log.Fatal("No password configured")
	}
	mailbox.Password = viper.GetString("password")

	log.Println("Opening database")
	mdb, err := lmdb.OpenMailDB("maildb.sqlite", &mailbox)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer mdb.Close()

	if false {
		log.Println("Opening imap connection")
		if err = mdb.ImapConnect(); err != nil {
			log.Fatalf("Connecting to the IMAP server: %v", err)
		}

		if err = mdb.Fetch(); err != nil {
			log.Fatalf("Fetching mail: %v", err)
		}
	} else {
		log.Println("Getting message roots")
		messages, err := mdb.GetMessageRoots()
		if err != nil {
			log.Fatalf("Getting message roots: %v", err)
		}

		var tgtMessage *lmdb.MessageTree
		tgtMessageId := "<example>"
		for _, message := range messages {
			log.Printf("%v | %v", message.Envelope.Date, message.Envelope.Subject)

			if message.Envelope.MessageId == tgtMessageId {
				tgtMessage = message
			}
		}

		log.Printf("Getting message tree for messageid %s", tgtMessageId)
		err = mdb.GetTree(tgtMessage)
		if err != nil {
			log.Fatalf("Getting message tree: %v", err)
		}

		TreePrint(tgtMessage, "")

	}
}
