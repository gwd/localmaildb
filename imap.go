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
	//
	// Also note: case must be taken to ensure that this DOES NOT
	// BLOCK if there are fetches further down the pipeline; otherwise
	// things may get backed up and deadlock.
	fetchreq := make(chan *fetchReq, 10)
	defer close(fetchreq)
	go func() {
		for req := range fetchreq {
			log.Printf("fetcher: Making request [%p]", req)
			req.done <- c.Fetch(req.seqset, req.items, req.messages)
		}
	}()

	// Get a list of all messages in INBOX
	envelopes := make(chan *imap.Message, 10)

	// Fetch envelopes in batches of 50, closing once they're all gone.
	go func() {
		STRIDE := uint32(50)
		from := uint32(1)

		envelopestatus := make(chan chan error, 1)

		go func() {
			for done := range envelopestatus {
				log.Printf("Waiting for channel %v to complete", done)
				err := <-done
				if err != nil {
					log.Printf("Envelope fetch error: %v", err)
				}
			}
			log.Printf("Closing envelopes")
			close(envelopes)
		}()

		for {
			if from > status.Messages {
				break
			}
			to := from + STRIDE
			if to > status.Messages {
				to = status.Messages
			}

			envreq := new(fetchReq)

			envreq.seqset = new(imap.SeqSet)
			envreq.seqset.AddRange(from, to)

			envreq.items = []imap.FetchItem{imap.FetchEnvelope}

			envreq.messages = make(chan *imap.Message, STRIDE)
			envreq.done = make(chan error, 1)

			go msgIgnoreClose(envreq.messages, envelopes)

			log.Printf("[%p] Reqesting envelopes (%d, %d)", envreq, from, to)

			fetchreq <- envreq
			envelopestatus <- envreq.done

			from = to + 1
		}

		log.Printf("Closing envelopestatus")
		close(envelopestatus)

	}()

	bodystatus := make(chan chan error, 1)

	go func() {
		requested := 0

		// cmsg: Message to check
		// emsg: Message from envelope
		// bmsg: Message from body
		for cmsg := range envelopes {
			if requested >= 10 {
				continue
			}

			if prs, err := mdb.IsMsgIdPresent(cmsg.Envelope.MessageId); err != nil {
				// FIXME: Not clear what the best thing would be to do
				// here.
				log.Fatalf("Checking message presence in database: %v", err)
			} else if prs {
				log.Printf(" Message %v present, not fetching", cmsg.Envelope.MessageId)
				continue
			}

			done := make(chan error)

			go func(done chan error, emsg *imap.Message) {
				// Request a single message
				bodyreq := new(fetchReq)
				bodyreq.seqset = new(imap.SeqSet)
				bodyreq.seqset.AddNum(emsg.SeqNum)
				bodyreq.messages = make(chan *imap.Message, 1)
				section := &imap.BodySectionName{}
				section.Peek = true
				bodyreq.items = []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}
				bodyreq.done = make(chan error, 1)

				log.Printf("[%p] Fetching message %s (%s)", bodyreq,
					emsg.Envelope.MessageId, emsg.Envelope.Subject)

				fetchreq <- bodyreq

				// Wait for the response
				bmsg := <-bodyreq.messages

				log.Printf("Processing body %v", bmsg.Envelope.MessageId)

				// Process
				if bmsg.SeqNum != emsg.SeqNum {
					log.Printf("Unexpected sequence number: wanted %d, got %d!",
						emsg.SeqNum, bmsg.SeqNum)
					done <- fmt.Errorf("Unexpected sequence number")
					return
				}
				if bmsg.Envelope.MessageId != emsg.Envelope.MessageId {
					log.Printf("Unexpected messageid: wanted %s, got %s!",
						emsg.Envelope.MessageId, bmsg.Envelope.MessageId)
					done <- fmt.Errorf("Unexpected message id")
					return
				}
				var body imap.Literal
				for _, body = range bmsg.Body {
				}
				if body == nil {
					done <- fmt.Errorf("No literals in message body")
					return
				}
				if err := mdb.AddMessage(bmsg.Envelope, body, int(bmsg.Size)); err != nil {
					done <- fmt.Errorf("Adding message to database: %v", err)
					return
				}

				done <- nil
			}(done, cmsg)

			bodystatus <- done

			requested++
		}

		close(bodystatus)

	}()

	log.Printf("Waiting for body processing statuses")
	for done := range bodystatus {
		log.Printf("Waiting for body status %v to complete", done)
		err := <-done
		if err != nil {
			log.Printf("Error processing body: %v", err)
		}
	}

	log.Printf("Done!")

	return nil
}
