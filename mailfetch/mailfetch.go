package main

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/viper"

	"github.com/emersion/go-mbox"

	imapsrc "github.com/gwd/localmaildb/imapsource"
	lmdb "github.com/gwd/localmaildb/localmaildb"
)

func TreePrint(message *lmdb.MessageTree, indent string) {
	log.Printf("%s %v %s", indent, message.Envelope.Date, message.Envelope.Subject)
	for _, reply := range message.Replies {
		TreePrint(reply, indent+"*")
	}

}

// Things to handle:
// - Only a single email, top-level has [PATCH] (or [PATCH v2])
// - Thread with [0/N] at the top
// - Thread with [1/N] at the top
func TreeAm(tree *lmdb.MessageTree) {
	mbw := mbox.NewWriter(os.Stdout)

	for _, reply := range tree.Replies {
		log.Printf("Envelope: %v", reply.Envelope)
		mbfrom := fmt.Sprintf("%s@%s", reply.Envelope.From[0].MailboxName, reply.Envelope.From[0].HostName)
		if w, err := mbw.CreateMessage(mbfrom, reply.Envelope.Date); err != nil {
			log.Fatalf("Creating message in mbox: %v", err)
		} else if _, err := w.Write(reply.RawMessage); err != nil {
			log.Fatalf("Writing message to mbox: %v", err)
		}
	}
}

func main() {
	mailbox := imapsrc.MailboxInfo{}

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

	var src imapsrc.ImapSource
	if src, err = imapsrc.Setup(&mailbox); err != nil {
		log.Fatalf("Error setting up imap source: %v", err)
	}

	log.Println("Opening database")
	mdb, err := lmdb.OpenMailDB("maildb.sqlite")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	defer mdb.Close()

	// FIXME: Eventually we want this done explicitly as part of an 'init' or 'create' step.
	if err := mdb.CreateMailbox(mailbox.MailboxName); err != nil {
		log.Fatalf("Error creating mailbox: %v", err)
	}

	cmd := "fetch"
	if len(os.Args) >= 2 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "fetch":
		log.Println("Opening imap connection")
		if err = src.ImapConnect(); err != nil {
			log.Fatalf("Connecting to the IMAP server: %v", err)
		}

		if err = src.Fetch(mdb); err != nil {
			log.Fatalf("Fetching mail: %v", err)
		}
	case "list-threads":
		log.Println("Getting message roots")
		messages, err := mdb.GetMessageRoots(mailbox.MailboxName)
		if err != nil {
			log.Fatalf("Getting message roots: %v", err)
		}

		for _, message := range messages {
			log.Printf("%v | %v | %v", message.Envelope.MessageId, message.Envelope.Date, message.Envelope.Subject)
		}
	case "list-thread":
		if len(os.Args) < 3 {
			log.Fatalf("Not enough arguments to %s", cmd)
		}
		tgtMessageId := os.Args[2]

		log.Println("Getting message roots")
		messages, err := mdb.GetMessageRoots(mailbox.MailboxName)
		if err != nil {
			log.Fatalf("Getting message roots: %v", err)
		}

		var tgtMessage *lmdb.MessageTree
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
	case "export-am":
		if len(os.Args) < 3 {
			log.Fatalf("Not enough arguments to %s", cmd)
		}
		tgtMessageId := os.Args[2]

		log.Println("Getting message roots")
		messages, err := mdb.GetMessageRoots(mailbox.MailboxName)
		if err != nil {
			log.Fatalf("Getting message roots: %v", err)
		}

		var tgtMessage *lmdb.MessageTree
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

		TreeAm(tgtMessage)

	default:
		log.Fatalf("Unknown command %s", cmd)
	}
}
