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

		log.Printf("Getting message tree for messageid %s", tgtMessageId)
		tgtMessage, err := mdb.GetTreeFromMessageId(tgtMessageId)
		if err != nil {
			log.Fatalf("Getting message tree: %v", err)
		}

		TreePrint(tgtMessage, "")
	case "export-am":
		if len(os.Args) < 3 {
			log.Fatalf("Not enough arguments to %s", cmd)
		}
		tgtMessageId := os.Args[2]

		log.Printf("Getting message tree for messageid %s", tgtMessageId)
		tgtMessage, err := mdb.GetTreeFromMessageId(tgtMessageId)
		if err != nil {
			log.Fatalf("Getting message tree: %v", err)
		}

		{
			mt := lmdb.TreeFilterAm(tgtMessage)
			mbw := mbox.NewWriter(os.Stdout)

			if len(mt) < 1 {
				log.Fatalf("No messages in thread after TreeFilterAm!")
			}

			for _, msg := range mt {
				log.Printf("Adding message %s", msg.Envelope.Subject)
				mbfrom := fmt.Sprintf("%s@%s", msg.Envelope.From[0].MailboxName, msg.Envelope.From[0].HostName)
				if w, err := mbw.CreateMessage(mbfrom, msg.Envelope.Date); err != nil {
					log.Fatalf("Creating message in mbox: %v", err)
				} else if _, err := w.Write(msg.RawMessage); err != nil {
					log.Fatalf("Writing message to mbox: %v", err)
				}
			}
		}

	default:
		log.Fatalf("Unknown command %s", cmd)
	}
}
