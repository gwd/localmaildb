package localmaildb

import (
	"database/sql"
	"fmt"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"log"
)

type ImapInfo struct {
	Hostname, Username, Password string
	Port                         int
}

type MailDB struct {
	db       *sql.DB
	imapinfo *ImapInfo
	client   *client.Client
}

func (mdb *MailDB) Close() {
	if mdb.db != nil {
		mdb.db.Close()
		mdb.db = nil
	}
	if mdb.client != nil {
		mdb.client.Logout()
		mdb.client = nil
	}
}

func (mdb *MailDB) ImapConnect() error {
	imapinfo := mdb.imapinfo

	if imapinfo == nil {
		return fmt.Errorf("IMAP dial info not set up")
	}

	if mdb.client != nil {
		// We already seem to have a connection -- see if it's still alive
		err := mdb.client.Noop()
		if err == nil {
			return nil
		}

		// This returned an error, which means the connection must
		// have failed; fall-through and attempt to make a new
		// connection
	}

	Port := mdb.imapinfo.Port
	// Default to 993 if no port is specifieds
	if Port == 0 {
		Port = 993
	}

	tgt := fmt.Sprintf("%s:%d", imapinfo.Hostname, Port)
	log.Printf("Dialing %s", tgt)
	c, err := client.DialTLS(tgt, nil)

	if err != nil {
		return fmt.Errorf("Attempting to connect to IMAP server: %v", err)
	}

	log.Printf("Logging in...")
	if err = c.Login(mdb.imapinfo.Username, mdb.imapinfo.Password); err != nil {
		return fmt.Errorf("Logging in to IMAP server: %v", err)
	}

	_, err = c.Select("INBOX", false)
	if err != nil {
		return err
	}

	mdb.client = c

	return nil
}

type fetchReq struct {
	seqset   *imap.SeqSet
	items    []imap.FetchItem
	messages chan *imap.Message
	done     chan error
}

func msgIgnoreClose(in chan *imap.Message, out chan *imap.Message) {
	for msg := range in {
		out <- msg
	}
}

func (mdb *MailDB) Fetch() error {
	// Connect or check the connection
	err := mdb.ImapConnect()
	if err != nil {
		return err
	}

	c := mdb.client

	// Get current status
	status := c.Mailbox()

	// NOTE: imap-client documentation says it's not safe for concurrent access.
	// Care must therefore be taken to make sure all fetch requests have completed
	// before using the client again.
	fetchreq := make(chan *fetchReq, 10)
	defer close(fetchreq)
	go func() {
		for req := range fetchreq {
			log.Println("fetcher: Making request")
			req.done <- c.Fetch(req.seqset, req.items, req.messages)
		}
	}()

	// Get a list of all messages in INBOX
	envelopes := make(chan *imap.Message, 10)

	go func() {
		STRIDE := uint32(50)
		from := uint32(1)

		// FIXME this seems kind of clunky
		envelopestatus := make(chan error, (status.Messages/STRIDE)+2)

		count := 0

		for {
			if from > status.Messages {
				break
			}
			to := from + STRIDE
			if to > status.Messages {
				to = status.Messages
			}

			envreq := new(fetchReq)

			log.Printf("Reqesting envelopes (%d, %d)", from, to)
			envreq.seqset = new(imap.SeqSet)
			envreq.seqset.AddRange(from, to)

			envreq.items = []imap.FetchItem{imap.FetchEnvelope}

			envreq.messages = make(chan *imap.Message, 10)
			envreq.done = envelopestatus

			go msgIgnoreClose(envreq.messages, envelopes)

			fetchreq <- envreq
			count += 1

			from = to + 1
		}

		//readenvelopes <- count
		for count > 0 {
			log.Printf("Waiting for %d envelope requests", count)
			err := <-envelopestatus
			if err != nil {
				log.Printf("Error fetching envelopes: %v", err)
			}
			count--
		}

		log.Printf("Closing envelopes")
		close(envelopes)
	}()

	bodyreq := new(fetchReq)

	bodyreq.seqset = new(imap.SeqSet)
	tofetch := []*imap.Message{}
	for msg := range envelopes {
		if len(tofetch) >= 10 {
			continue
		}
		log.Printf("Processing msgid %s: %s", msg.Envelope.MessageId, msg.Envelope.Subject)
		if prs, err := mdb.IsMsgIdPresent(msg.Envelope.MessageId); err != nil {
			// FIXME: Not clear what the best thing would be to do
			// here.
			log.Fatalf("Checking message presence in database: %v", err)
		} else if !prs {
			log.Printf(" Messageid not present, adding to fetch list")
			tofetch = append(tofetch, msg)
			bodyreq.seqset.AddNum(msg.SeqNum)
		} else {
			log.Printf(" Messageid present, not fetching")
		}
	}

	log.Printf("Messages to retreive: %v", tofetch)

	if len(tofetch) == 0 {
		log.Printf("No not-present messages")
		return nil
	}

	log.Printf("Fetching %d messages", len(tofetch))
	bodyreq.messages = make(chan *imap.Message, 10)
	section := &imap.BodySectionName{}
	section.Peek = true
	bodyreq.items = []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}
	bodyreq.done = make(chan error, 1)

	fetchreq <- bodyreq

	i := 0
	for msg := range bodyreq.messages {
		log.Printf("Processing seqnum %d", msg.SeqNum)
		if msg.SeqNum != tofetch[i].SeqNum {
			log.Printf("Unexpected sequence number: wanted %d, got %d!",
				tofetch[i].SeqNum, msg.SeqNum)
			return fmt.Errorf("Unexpected sequence number")
		}
		if msg.Envelope.MessageId != tofetch[i].Envelope.MessageId {
			log.Printf("Unexpected messageid: wanted %s, got %s!",
				tofetch[i].Envelope.MessageId, msg.Envelope.MessageId)
			return fmt.Errorf("Unexpected message id")
		}
		var body imap.Literal
		for _, body = range msg.Body {
		}
		if body == nil {
			log.Fatalf("No literals in message body")
		}
		if err := mdb.AddMessage(msg.Envelope, body, int(msg.Size)); err != nil {
			// FIXME: Do something more graceful (not clear what the best thing woudl be to do)
			log.Fatalf("Adding message to database: %v", err)
		}
		i++
	}

	if err := <-bodyreq.done; err != nil {
		return err
	}

	return nil
}
