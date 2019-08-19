package localmaildb

import (
	"database/sql"
	"fmt"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"log"
	"time"
)

type UpdateStrategy int

const (
	StrategyAll    = UpdateStrategy(1)
	StrategyRecent = UpdateStrategy(2)
)

type MailboxInfo struct {
	MailboxName string // Required
	UpdateStrategy
	Hostname, Username, Password string
	Port                         int
	UpdateWindow                 time.Duration
	mailboxId                    int // ID in database
}

type MailDB struct {
	db      *sql.DB
	mailbox MailboxInfo
	client  *client.Client

	// Processing context
	fetchreq       chan *fetchReq
	bodyStatusChan chan chan error
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
	imapinfo := &mdb.mailbox

	if imapinfo.Hostname == "" {
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

	Port := imapinfo.Port
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
	if err = c.Login(imapinfo.Username, imapinfo.Password); err != nil {
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

// Start a go routine for handling fetch requests.  Close the returned channel when done.
func (mdb *MailDB) goFetch() chan *fetchReq {
	mdb.fetchreq = make(chan *fetchReq, 10)
	go func() {
		c := mdb.client
		fetchreq := mdb.fetchreq
		for req := range fetchreq {
			log.Printf("fetcher: Making request [%p]", req)
			req.done <- c.Fetch(req.seqset, req.items, req.messages)
		}
	}()
	return mdb.fetchreq
}

func (mdb *MailDB) goFetchClose() {
	close(mdb.fetchreq)
	mdb.fetchreq = nil
}

func (mdb *MailDB) goFetchEnvelopeBatches(Messages uint32) (chan chan error, chan []string) {
	mdb.bodyStatusChan = make(chan chan error, 1)
	messageIds := make(chan []string, 1)

	go func() {
		envelopeBatchChan := make(chan chan []string, 1)

		// The caller wants to wait until all body fetch operations have
		// finished; but we don't know how many there are.  We can't close
		// bodyStatusChan until we know that no more bodyStatus channels
		// will be sent down it.  So we wait until all envelope batches
		// have been handled.
		go func() {
			allMessages := []string{}
			for envelopeBatch := range envelopeBatchChan {
				messages := <-envelopeBatch
				allMessages = append(allMessages, messages...)
			}
			close(mdb.bodyStatusChan)
			mdb.bodyStatusChan = nil
			messageIds <- allMessages
			close(messageIds)
		}()

		STRIDE := uint32(50)
		from := uint32(1)

		for {
			if from > Messages {
				break
			}
			to := from + STRIDE
			if to > Messages {
				to = Messages
			}

			envreq := new(fetchReq)

			envreq.seqset = new(imap.SeqSet)
			envreq.seqset.AddRange(from, to)

			envreq.items = []imap.FetchItem{imap.FetchEnvelope}

			envreq.messages = make(chan *imap.Message, STRIDE)
			envreq.done = make(chan error, 1)

			log.Printf("[%p] Reqesting envelopes (%d, %d)", envreq, from, to)

			mdb.fetchreq <- envreq

			envelopeBatch := make(chan []string, 1)

			go mdb.goProcessEnvelopeBatch(envreq, envelopeBatch)

			envelopeBatchChan <- envelopeBatch

			from = to + 1
		}

		// No more envelope batches will be created.
		close(envelopeBatchChan)
	}()

	return mdb.bodyStatusChan, messageIds
}

// Go routine to process the outcome of the message
func (mdb *MailDB) goProcessEnvelopeBatch(envreq *fetchReq, envelopeBatch chan []string) {
	messages := []string{}

	// cmsg: Message to check
	// emsg: Message from envelope
	// bmsg: Message from body
	for cmsg := range envreq.messages {
		messages = append(messages, cmsg.Envelope.MessageId)
		if prs, err := mdb.IsMsgIdPresent(cmsg.Envelope.MessageId); err != nil {
			// FIXME: Not clear what the best thing would be to do
			// here.
			log.Fatalf("Checking message presence in database: %v", err)
		} else if prs {
			//log.Printf(" Message %v present, not fetching", cmsg.Envelope.MessageId)
			continue
		}

		bodyStatus := make(chan error, 1)

		go mdb.goProcessBody(cmsg, bodyStatus)

		mdb.bodyStatusChan <- bodyStatus
	}

	err := envreq.done
	if err != nil {
		log.Printf("Envelope fetch error: %v", err)
	}

	envelopeBatch <- messages
}

func (mdb *MailDB) goProcessBody(emsg *imap.Message, bodyStatus chan error) {
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

	mdb.fetchreq <- bodyreq

	// Wait for the response
	bmsg := <-bodyreq.messages

	log.Printf("Processing body %v", bmsg.Envelope.MessageId)

	// Process
	if bmsg.SeqNum != emsg.SeqNum {
		log.Printf("Unexpected sequence number: wanted %d, got %d!",
			emsg.SeqNum, bmsg.SeqNum)
		bodyStatus <- fmt.Errorf("Unexpected sequence number")
		return
	}
	if bmsg.Envelope.MessageId != emsg.Envelope.MessageId {
		log.Printf("Unexpected messageid: wanted %s, got %s!",
			emsg.Envelope.MessageId, bmsg.Envelope.MessageId)
		bodyStatus <- fmt.Errorf("Unexpected message id")
		return
	}
	var body imap.Literal
	for _, body = range bmsg.Body {
	}
	if body == nil {
		bodyStatus <- fmt.Errorf("No literals in message body")
		return
	}
	if err := mdb.AddMessage(bmsg.Envelope, body); err != nil {
		bodyStatus <- fmt.Errorf("Adding message to database: %v", err)
		return
	}

	bodyStatus <- nil
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
	mdb.goFetch()
	defer mdb.goFetchClose()

	// Fetch envelopes in batches of 50, closing once they're all gone.
	bodyStatusChan, messageIdChan := mdb.goFetchEnvelopeBatches(status.Messages)

	log.Printf("Waiting for body processing statuses")
	for bodyStatus := range bodyStatusChan {
		log.Printf("Waiting for body status %v to complete", bodyStatus)
		err := <-bodyStatus
		if err != nil {
			log.Printf("Error processing body: %v", err)
		}
	}

	messageIds := <-messageIdChan

	log.Printf("Inbox contains %d messages", len(messageIds))

	mdb.updateMailbox(messageIds)

	log.Printf("Done!")

	return nil
}
