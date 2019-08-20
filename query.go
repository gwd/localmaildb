package localmaildb

import (
	"fmt"
	"github.com/emersion/go-imap"
	"log"
	"time"
)

type MessageTree struct {
	RawMessage       []byte
	Envelope         imap.Envelope
	Replies          []*MessageTree
	Earliest, Latest time.Time
}

// FIXME:
func (mdb *MailDB) GetMessageRoots() ([]*MessageTree, error) {
	db := mdb.db

	mboxid := mdb.mailbox.mailboxId
	// FIXME
	if mboxid == 0 {
		return nil, fmt.Errorf("No mailbox id!")
	}

	// Find all messages in the inbox.
	// Find the "roots" of all these trees
	// Return them.
	rows, err := db.Query(`
        WITH RECURSIVE
            ancestor(messageid) AS
                (select messageid 
                     from lmdb_mailbox_join
                     where mailboxid=?
                 union
                 select lmdb_messages.inreplyto
                     from lmdb_messages join ancestor using(messageid))
        select self.messageid, self.subject, self.date, self.message
            from lmdb_messages as self
                left join lmdb_messages as parent
                on self.inreplyto = parent.messageid
        where self.messageid in ancestor
              and IFNULL(parent.messageid, TRUE)`, mboxid)
	if err != nil {
		log.Printf("Error getting 'root' message list: %v", err)
		return nil, err
	}

	messages := []*MessageTree{}

	for rows.Next() {
		message := &MessageTree{}
		var dateSeconds int64
		var messageString string
		err = rows.Scan(&message.Envelope.MessageId,
			&message.Envelope.Subject,
			&dateSeconds,
			&messageString)
		if err != nil {
			log.Printf("Scanning results: %v", err)
			return nil, err
		}
		message.Envelope.Date = time.Unix(dateSeconds, 0)
		message.Latest = message.Envelope.Date
		message.RawMessage = []byte(messageString)
		messages = append(messages, message)
	}

	return messages, nil
}
